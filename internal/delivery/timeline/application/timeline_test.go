package application

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func TestTimelineCitesTheChainAndSkipsAnotherEnvironment(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	early := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	later := early.Add(time.Hour)
	for _, event := range []governance.AuthorizationAuditEvent{
		governance.NewAuthorizationCheckEvent(early, "ada", "fleet.propose_fix", "environment/east/installation/ledger", governance.DecisionAllow, "drift", nil),
		governance.NewAuthorizationCheckEvent(later, "bea", "blackout.decision", "environment/west", governance.DecisionDeny, "other", nil),
	} {
		if _, err := repository.AppendAuthorizationAudit(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.CreatePlan(ctx, controlplane.PlanRecord{
		ID: "pln_east", CreatedAt: later.Add(time.Minute), Status: controlplane.PlanSucceeded,
		Plan: controlplane.Plan{Environment: "east"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := Build(ctx, repository, "east")
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows = %#v %v", rows, err)
	}
	if rows[0].Kind != "plan" || rows[1].Kind != "audit" || !rows[1].Verified || rows[1].Digest == "" || rows[1].Sequence == 0 {
		t.Fatalf("order = %#v", rows)
	}
	events, err := repository.ListAuthorizationAudit(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	events[0].ID = events[0].ID + "-tampered"
	chain, err := controlplane.ChainAuthorizationAudit(events)
	if err != nil {
		t.Fatal(err)
	}
	if chain[0].Digest == rows[1].Digest && chain[0].ID == rows[1].Summary {
		t.Fatal("tampered chain should not keep the original identity")
	}
}
