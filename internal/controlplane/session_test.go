package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestSessionProjectionIsBoundedAndOmitsIdentityClaims(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	ctx := context.Background()
	if _, err := governance.PutTeam(ctx, Team{
		Name: "viewers", DisplayName: "Viewers", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: "environment/dev", Team: "viewers", Roles: []string{"environment:viewer"},
	}); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	request := httptest.NewRequest(http.MethodGet, "/v1/session?resourceRid=environment/dev&resourceRid=product/app.example:payments", nil)
	request.Header.Set("X-Platform-Subject", "alice")
	request.Header.Set("X-Platform-Groups", "sensitive-idp-group")
	request.Header.Set("Authorization", "Bearer must-not-appear")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var projection SessionProjection
	if err := json.Unmarshal(response.Body.Bytes(), &projection); err != nil {
		t.Fatal(err)
	}
	if projection.Actor.Username != "alice" || projection.Effective.Username != "alice" || projection.Delegated {
		t.Fatalf("principal projection = %#v", projection)
	}
	if strings.Contains(response.Body.String(), "sensitive-idp-group") || strings.Contains(response.Body.String(), "must-not-appear") || strings.Contains(response.Body.String(), "Authorization") {
		t.Fatalf("session leaked identity claims or credentials: %s", response.Body.String())
	}
	if len(projection.Resources) != 2 || projection.Resources[0].ResourceRID != "environment/dev" ||
		!contains(projection.Resources[0].Operations, OperationReadResource) ||
		len(projection.Resources[1].Operations) != 0 {
		t.Fatalf("resource capabilities = %#v", projection.Resources)
	}
	globalOnly := map[string]bool{"fleet": true, "promotions": true, "risk": true, "export": true, "import": true}
	teamsAdvertised := false
	for _, application := range projection.Applications {
		if globalOnly[application.ID] && application.Enabled {
			t.Fatalf("global-only application enabled without global read: %#v", projection.Applications)
		}
		if application.ID == "teams" {
			teamsAdvertised = true
			if !application.Enabled {
				t.Fatalf("teams is readable by any operator and must stay enabled: %#v", projection.Applications)
			}
		}
	}
	// Every console screen must be advertised, or its rail entry can never appear.
	for _, id := range []string{"change-requests", "fleet", "plans", "promotions", "teams", "risk", "export", "import"} {
		if !slices.ContainsFunc(projection.Applications, func(a SessionApplication) bool { return a.ID == id }) {
			t.Fatalf("session does not advertise the %q application; its rail entry is unreachable", id)
		}
	}
	if !teamsAdvertised {
		t.Fatal("teams application was not advertised")
	}
}

func TestSessionProjectionAdministratorAndInputBounds(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"},
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/session?resourceRid=environment/dev", nil)
	request.Header.Set("X-Platform-Subject", "admin")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var projection SessionProjection
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &projection) != nil {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(projection.GlobalOperations) != len(sessionOperationVocabulary) ||
		len(projection.Resources) != 1 || len(projection.Resources[0].Operations) != len(sessionOperationVocabulary) {
		t.Fatalf("administrator capabilities = %#v", projection)
	}
	for _, application := range projection.Applications {
		if !application.Enabled {
			t.Fatalf("administrator application disabled: %#v", projection.Applications)
		}
	}

	tooMany := make([]string, maximumSessionResourceQueries+1)
	for index := range tooMany {
		tooMany[index] = "resourceRid=environment/e" + strings.Repeat("x", index+1)
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/session?"+strings.Join(tooMany, "&"), nil)
	request.Header.Set("X-Platform-Subject", "admin")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unbounded query status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSessionProjectionDistinguishesDelegatedIdentityAndCapsTeams(t *testing.T) {
	t.Parallel()
	teams := make([]string, maximumSessionTeams+1)
	for index := range teams {
		teams[index] = "team-" + strings.Repeat("x", index+1)
	}
	projection, err := projectSession(
		context.Background(),
		NewGovernanceService(NewMemoryRepository()),
		Principal{
			Username: "actor",
			OnBehalfOf: &Principal{
				Username:       "delegate",
				Teams:          teams,
				ServiceAccount: true,
			},
		},
		HandlerOptions{},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !projection.Delegated || projection.Actor.Username != "actor" ||
		projection.Effective.Username != "delegate" || !projection.Effective.ServiceAccount {
		t.Fatalf("delegated identity = %#v", projection)
	}
	if len(projection.Effective.Teams) != maximumSessionTeams || !projection.Effective.TeamsTruncated {
		t.Fatalf("bounded effective teams = %#v", projection.Effective)
	}
}

func TestSessionRequiresAuthenticatedOperatorIdentity(t *testing.T) {
	t.Parallel()
	response := httptest.NewRecorder()
	NewHandler(NewMemoryRepository()).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/session", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSessionResourceScopeIsMetadataOnlyAndFailsClosed(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), Team{
		Name: "viewers", DisplayName: "Viewers", AdminUsers: []string{"team-admin"}, MemberUsers: []string{"alice"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), ResourceGrant{
		ResourceRID: "environment/dev", Team: "viewers", Roles: []string{"environment:viewer"},
	}); err != nil {
		t.Fatal(err)
	}
	requested := []string{"known-alias", "unknown-alias", "error-alias", "empty-alias", "environment/dev"}
	configuration := HandlerOptions{
		BootstrapAdministrators: []string{"root"},
		SessionResourceScope: func(_ context.Context, resource string) (string, bool, error) {
			switch resource {
			case "known-alias":
				return "environment/dev", true, nil
			case "unknown-alias":
				return "environment/dev", false, nil
			case "error-alias":
				return "environment/dev", true, errors.New("inventory unavailable")
			case "empty-alias":
				return "", true, nil
			case "environment/dev":
				return resource, true, nil
			default:
				t.Fatalf("resolver received unrequested resource %q", resource)
				return "", false, nil
			}
		},
	}
	for _, actor := range []string{"alice", "root"} {
		principal := Principal{Username: actor, Teams: []string{"viewers"}}
		projection, err := projectSession(t.Context(), governance, principal, configuration, requested)
		if err != nil {
			t.Fatal(err)
		}
		for _, resource := range projection.Resources {
			readable := resource.ResourceRID == "known-alias" || resource.ResourceRID == "environment/dev"
			if contains(resource.Operations, OperationReadResource) != readable || (!readable && len(resource.Operations) != 0) {
				t.Fatalf("%s capability metadata failed closed resolution: %#v", actor, resource)
			}
		}
		if len(projection.GlobalOperations) != 0 && actor == "alice" {
			t.Fatalf("alias expanded global capabilities: %#v", projection.GlobalOperations)
		}
	}
	// The optional projection port cannot become an authorization shortcut for
	// another route. Only its ordinary native coordinate receives the grant.
	alice := Principal{Username: "alice", Teams: []string{"viewers"}}
	if governance.AuthorizeResourceOperation(t.Context(), alice, configuration, "known-alias", OperationReadResource) ||
		!governance.AuthorizeResourceOperation(t.Context(), alice, configuration, "environment/dev", OperationReadResource) {
		t.Fatal("session metadata resolver changed native route authorization")
	}
}
