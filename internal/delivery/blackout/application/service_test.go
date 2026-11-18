package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestDeclareRequiresConfirmationAndClearRefusesTheAuthor(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 14, 18, 0, 0, 0, time.UTC)
	service := &Service{Store: repository, Now: func() time.Time { return now }}
	start, end := now, now.Add(time.Hour)
	denied, err := service.Declare(ctx, "ada", "east", "change freeze for the rehearsal", start, end, false)
	if err != nil || denied.Allowed || denied.Reason != "confirmation is required" {
		t.Fatalf("confirm = %#v %v", denied, err)
	}
	declared, err := service.Declare(ctx, "ada", "east", "change freeze for the rehearsal", start, end, true)
	if err != nil || !declared.Allowed || service.Active("east", start) != "change freeze for the rehearsal" {
		t.Fatalf("declare = %#v %v", declared, err)
	}
	if service.Active("east", end) != "" {
		t.Fatal("outside the blackout")
	}
	proposed, err := service.ProposeClear(ctx, controlplane.Principal{Username: "ada"}, "east")
	if err != nil || proposed.ChangeRequestID == "" {
		t.Fatalf("propose = %#v %v", proposed, err)
	}
	self, err := service.ApproveClear(ctx, controlplane.Principal{Username: "ada"}, proposed.ChangeRequestID, "mine")
	if err != nil || self.Allowed || service.Active("east", start) == "" {
		t.Fatalf("self = %#v active %q %v", self, service.Active("east", start), err)
	}
	cleared, err := service.ApproveClear(ctx, controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}, proposed.ChangeRequestID, "rehearsal is over")
	if err != nil || !cleared.Allowed || service.Active("east", start) != "" {
		t.Fatalf("clear = %#v active %q %v", cleared, service.Active("east", start), err)
	}
}

// Embedding preserves the real repository contract while failing only audit writes.
type declarationAuditFailure struct {
	controlplane.Repository
	refusal error
}

func (store declarationAuditFailure) AppendAuthorizationAudit(context.Context, controlplane.AuthorizationAuditEvent) (controlplane.AuthorizationAuditEvent, error) {
	return controlplane.AuthorizationAuditEvent{}, store.refusal
}
func TestFailedDeclarationAuditLeavesActiveBlackoutsUnchanged(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	repository := controlplane.NewMemoryRepository()
	service := &Service{Store: repository, Now: func() time.Time { return now }}
	existing, err := service.Declare(t.Context(), "ada", "east", "existing freeze", now, now.Add(time.Hour), true)
	if err != nil || !existing.Allowed || existing.AuditID == "" {
		t.Fatalf("initial declaration=%+v %v", existing, err)
	}
	refusal := errors.New("audit storage unavailable")
	service.Store = declarationAuditFailure{Repository: repository, refusal: refusal}
	failed, err := service.Declare(t.Context(), "bea", "west", "must not become active", now, now.Add(time.Hour), true)
	if !errors.Is(err, refusal) || failed.Allowed || failed.AuditID != "" || len(failed.Blackouts) != 0 {
		t.Fatalf("failed declaration=%+v %v", failed, err)
	}
	if got := service.Active("west", now); got != "" {
		t.Fatalf("failed declaration became active: %q", got)
	}
	if got := service.Active("east", now); got != "existing freeze" {
		t.Fatalf("existing declaration changed: %q", got)
	}
	// Once audit recording recovers, the same request can succeed exactly once.
	service.Store = repository
	recovered, err := service.Declare(t.Context(), "bea", "west", "reviewed freeze", now, now.Add(time.Hour), true)
	if err != nil || !recovered.Allowed || recovered.AuditID == "" || len(recovered.Blackouts) != 2 || service.Active("west", now) != "reviewed freeze" {
		t.Fatalf("recovered declaration=%+v %v", recovered, err)
	}
}
