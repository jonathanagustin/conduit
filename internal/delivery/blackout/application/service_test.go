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

// Record real governance calls so stale requests cannot silently reach approval.
type clearApprovalRecorder struct {
	Governance
	calls []string
}

func (recorder *clearApprovalRecorder) Approve(ctx context.Context, id string, actor controlplane.Principal, rationale string) (controlplane.ChangeRequest, controlplane.PlanRecord, error) {
	recorder.calls = append(recorder.calls, id)
	return recorder.Governance.Approve(ctx, id, actor, rationale)
}
func TestPendingClearsKeepTheirOriginalTargetAfterRemoval(t *testing.T) {
	for _, tail := range []bool{false, true} {
		name := "last index remains safe"
		if tail {
			name = "unrelated later blackout stays active"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
			repository := controlplane.NewMemoryRepository()
			service := &Service{Store: repository, Now: func() time.Time { return now }}
			declare := func(environment string) {
				t.Helper()
				got, err := service.Declare(t.Context(), "ada", environment, environment+" freeze", now, now.Add(time.Hour), true)
				if err != nil || !got.Allowed {
					t.Fatalf("declare %s=%+v %v", environment, got, err)
				}
			}
			declare("east")
			declare("west")
			if tail {
				declare("north")
			}
			propose := func(environment string) string {
				t.Helper()
				got, err := service.ProposeClear(t.Context(), controlplane.Principal{Username: "ada"}, environment)
				if err != nil || got.ChangeRequestID == "" {
					t.Fatalf("propose %s=%+v %v", environment, got, err)
				}
				return got.ChangeRequestID
			}
			east, west := propose("east"), propose("west")
			reviewer := controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}
			for _, id := range []string{east, west} {
				got, err := service.ApproveClear(t.Context(), reviewer, id, "reviewed target")
				if err != nil || !got.Allowed {
					t.Fatalf("clear %s=%+v %v", id, got, err)
				}
			}
			if service.Active("east", now) != "" || service.Active("west", now) != "" {
				t.Fatal("approved target remains active")
			}
			if tail && service.Active("north", now) != "north freeze" {
				t.Fatal("approval removed an unrelated blackout")
			}
		})
	}
}
func TestClearedRequestCannotApproveAReplacementBlackout(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	repository := controlplane.NewMemoryRepository()
	recorder := &clearApprovalRecorder{Governance: controlplane.NewGovernanceService(repository)}
	service := &Service{Store: repository, Governance: recorder, Now: func() time.Time { return now }}
	declare := func(reason string) {
		t.Helper()
		got, err := service.Declare(t.Context(), "ada", "east", reason, now, now.Add(time.Hour), true)
		if err != nil || !got.Allowed {
			t.Fatalf("declare=%+v %v", got, err)
		}
	}
	declare("original freeze")
	first, err := service.ProposeClear(t.Context(), controlplane.Principal{Username: "ada"}, "east")
	if err != nil || first.ChangeRequestID == "" {
		t.Fatalf("first=%+v %v", first, err)
	}
	reviewer := controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}
	cleared, err := service.ApproveClear(t.Context(), reviewer, first.ChangeRequestID, "reviewed original")
	if err != nil || !cleared.Allowed {
		t.Fatalf("clear=%+v %v", cleared, err)
	}
	declare("replacement freeze")
	calls := len(recorder.calls)
	stale, err := service.ApproveClear(t.Context(), reviewer, first.ChangeRequestID, "old review")
	if err != nil || stale.Allowed || stale.Reason != "change request is not pending" || len(recorder.calls) != calls || service.Active("east", now) != "replacement freeze" {
		t.Fatalf("stale review changed replacement: %+v %v calls=%v", stale, err, recorder.calls)
	}
}
