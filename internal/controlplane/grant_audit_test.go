package controlplane

import (
	"context"
	"net/http"
	"testing"
)

// Permission changes were half-audited. requireResourceOperation emits an
// authorizationCheck for permissions:manage on every grant mutation, ALLOW and
// DENY both -- so who touched permissions and when was answerable. What the
// grant became was not: PutGrant overwrites in place with Revision+1 and
// retains no history, and the handler added nothing.
//
// So "who granted the approver role that let this change through" could not be
// answered from the audit trail, in a system whose central claim is that
// changes are reviewed by the right people.

func grantAuditEvents(t *testing.T, repository Repository, action string) []AuthorizationAuditEvent {
	t.Helper()
	events, err := repository.ListAuthorizationAudit(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	matched := []AuthorizationAuditEvent{}
	for _, event := range events {
		if event.Action == action {
			matched = append(matched, event)
		}
	}
	return matched
}

// auditedRoles reads the roles out of a grant snapshot as they survive the
// audit record's JSON round trip, which is the form any reader of the trail
// actually gets. Asserting on []string would pass in memory and say nothing
// about what a deployment stores.
func auditedRoles(t *testing.T, snapshot any) []string {
	t.Helper()
	fields, ok := snapshot.(map[string]any)
	if !ok {
		t.Fatalf("grant snapshot is not an object: %#v", snapshot)
	}
	roles := []string{}
	switch values := fields["roles"].(type) {
	case []string:
		roles = append(roles, values...)
	case []any:
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				t.Fatalf("a recorded role is not a string: %#v", value)
			}
			roles = append(roles, text)
		}
	default:
		t.Fatalf("grant snapshot records no roles: %#v", fields)
	}
	return roles
}

func TestAGrantMutationRecordsWhatItBecameAndWhoDidIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	if _, err := service.PutTeam(ctx, Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrantAs(ctx, ResourceGrant{
		ResourceRID: environmentRID("prod"), Team: "reviewers",
		Roles: []string{"environment:viewer"},
	}, "alice"); err != nil {
		t.Fatal(err)
	}

	events := grantAuditEvents(t, repository, "RESOURCE_GRANT_CONFIGURE")
	if len(events) != 1 {
		t.Fatalf("expected one grant-configuration event, got %d", len(events))
	}
	event := events[0]
	if event.Subject != "alice" {
		t.Errorf("the event does not name who conferred the grant: %q", event.Subject)
	}
	if event.ResourceRID != environmentRID("prod") {
		t.Errorf("the event is not anchored to the resource: %q", event.ResourceRID)
	}
	roles := auditedRoles(t, event.RequestFields["grantAfter"])
	if len(roles) != 1 || roles[0] != "environment:viewer" {
		t.Errorf("the event does not say what the grant became: %#v", roles)
	}
}

// The before half is what makes the record usable. An event saying only what a
// grant became cannot answer whether an operation was added or removed, and
// removal is the direction someone covering their tracks would use.
func TestAGrantMutationRecordsWhatItWasBefore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	if _, err := service.PutTeam(ctx, Team{Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	first, err := service.PutGrantAs(ctx, ResourceGrant{
		ResourceRID: environmentRID("prod"), Team: "reviewers",
		Roles: []string{"environment:administrator"},
	}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	// The revision is carried so the second write is an update rather than a
	// conflict, which is the case the before-state exists to describe.
	if _, err = service.PutGrantAs(ctx, ResourceGrant{
		ResourceRID: environmentRID("prod"), Team: "reviewers",
		Roles: []string{"environment:viewer"}, Revision: first.Revision,
	}, "mallory"); err != nil {
		t.Fatal(err)
	}

	events := grantAuditEvents(t, repository, "RESOURCE_GRANT_CONFIGURE")
	if len(events) != 2 {
		t.Fatalf("expected two grant-configuration events, got %d", len(events))
	}
	demotion := events[1]
	if demotion.Subject != "mallory" {
		t.Fatalf("the second event names the wrong principal: %q", demotion.Subject)
	}
	roles := auditedRoles(t, demotion.RequestFields["grantBefore"])
	if len(roles) != 1 || roles[0] != "environment:administrator" {
		t.Fatalf("the removed role is not recoverable from the audit trail: %#v", roles)
	}
}

// The route is the only production caller, so the wiring is the whole of what
// makes this reach a deployment. Asserted end to end rather than on the service
// alone, because a service that records faithfully behind a handler that never
// calls it is the defect in a quieter form.
func TestTheResourceGrantRouteRecordsTheChangeItMade(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := requestJSON(t, handler, http.MethodPut, "/v1/teams/reviewers",
		map[string]any{"displayName": "Reviewers", "adminUsers": []string{"alice"}, "memberUsers": []string{"alice"}})
	if response.Code != http.StatusOK {
		t.Fatalf("create team = %d %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost, "/v1/resource-grants", map[string]any{
		"resourceRid": environmentRID("prod"), "team": "reviewers", "roles": []string{"environment:viewer"},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("put grant = %d %s", response.Code, response.Body.String())
	}

	if events := grantAuditEvents(t, repository, "RESOURCE_GRANT_CONFIGURE"); len(events) != 1 {
		t.Fatalf("the route wrote a grant and recorded no content: %d event(s)", len(events))
	}
}

// The authorization decision is still audited separately, and must stay that
// way. The two events answer different questions -- one whether the principal
// was allowed, the other what they did -- and collapsing them would lose the
// DENY case, which has no grant to describe.
func TestTheAuthorizationCheckSurvivesBesideTheContentRecord(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	if response := requestJSON(t, handler, http.MethodPut, "/v1/teams/reviewers",
		map[string]any{"displayName": "Reviewers", "adminUsers": []string{"alice"}, "memberUsers": []string{"alice"}}); response.Code != http.StatusOK {
		t.Fatalf("create team = %d %s", response.Code, response.Body.String())
	}
	if response := requestJSON(t, handler, http.MethodPost, "/v1/resource-grants", map[string]any{
		"resourceRid": environmentRID("prod"), "team": "reviewers", "roles": []string{"environment:viewer"},
	}); response.Code != http.StatusOK {
		t.Fatalf("put grant = %d %s", response.Code, response.Body.String())
	}

	if events := grantAuditEvents(t, repository, OperationManagePermissions); len(events) == 0 {
		t.Fatal("the authorization check for permissions:manage is no longer recorded")
	}
}
