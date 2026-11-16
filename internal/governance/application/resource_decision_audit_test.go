package application

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"testing"
	"time"
)

type decisionAuditProbe struct {
	err    error
	events []domain.AuthorizationAuditEvent
}

func (sink *decisionAuditProbe) AppendAuthorizationAudit(_ context.Context, event domain.AuthorizationAuditEvent) (domain.AuthorizationAuditEvent, error) {
	sink.events = append(sink.events, event)
	return event, sink.err
}

func TestResourceMutationRequiresPersistedDecision(t *testing.T) {
	operations := append(append([]string{}, domain.OperationVocabulary...), "future:operation")
	for _, actor := range []string{"member", "root", "robot", "outsider"} {
		for _, outage := range []bool{false, true} {
			for _, operation := range operations {
				t.Run(actor+"/"+operation+"/"+map[bool]string{true: "outage", false: "healthy"}[outage], func(t *testing.T) {
					sink := &decisionAuditProbe{}
					if outage {
						sink.err = errors.New("audit unavailable")
					}
					grants := &recordingGrants{byScope: map[string][]domain.ResourceGrant{"environment/dev": {{ID: "grant", Team: "ops", Operations: operations}}}}
					principal := domain.Principal{Username: actor}
					if actor == "member" {
						principal.Teams = []string{"ops"}
					}
					if actor == "robot" {
						principal.OnBehalfOf = &domain.Principal{Username: "member", Teams: []string{"ops"}}
					}
					now := time.Date(2026, 11, 16, 13, 0, 0, 123456789, time.UTC)
					decision := ResourceDecision{Authorizer: NewAuthorizer(grants), Audit: sink, Policy: AdministratorPolicy{BootstrapAdministrators: []string{"root"}}, Now: func() time.Time { return now }}
					want := actor != "outsider" && (!outage || operation == domain.OperationReadResource || operation == domain.OperationReadDebugRequest)
					if got := decision.Allow(t.Context(), principal, "environment/dev", operation); got != want {
						t.Fatalf("admission=%v, want %v", got, want)
					}
					if len(sink.events) != 1 {
						t.Fatalf("audit attempts=%d, want 1", len(sink.events))
					}
					event := sink.events[0]
					verdict := domain.DecisionAllow
					if actor == "outsider" {
						verdict = domain.DecisionDeny
					}
					if event.Subject != actor || event.Action != operation || event.ResourceRID != "environment/dev" || event.Decision != verdict || !event.CreatedAt.Equal(now) {
						t.Fatalf("decision carrier or evidence changed: %#v", event)
					}
					represented := ""
					if actor == "robot" {
						represented = "member"
					}
					if event.Context["onBehalfOf"] != represented {
						t.Fatalf("delegation changed: %#v", event.Context)
					}
				})
			}
		}
	}
}

func TestResourceDecisionWithoutAuditSinkRefusesAdmission(t *testing.T) {
	decision := ResourceDecision{Policy: AdministratorPolicy{BootstrapAdministrators: []string{"root"}}}
	for _, operation := range []string{domain.OperationOperate, domain.OperationReadResource} {
		if decision.Allow(t.Context(), domain.Principal{Username: "root"}, "environment/dev", operation) {
			t.Fatal("missing required sink admitted operation")
		}
	}
}
