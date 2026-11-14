package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type memoryOpen struct{ ids []string }

func (open *memoryOpen) OpenPending(_ context.Context, change PendingChange) (string, error) {
	if change.Author == "" || change.Value == "" {
		return "", errors.New("incomplete")
	}
	open.ids = append(open.ids, change.TargetID)
	return "crq_test", nil
}

type memoryAudit struct{ rows []string }

func (audit *memoryAudit) Record(_ context.Context, runID string, provenance domain.Provenance, outcome, reason string) error {
	audit.rows = append(audit.rows, runID+" "+provenance.Adapter+" "+provenance.PromptSHA256+" "+provenance.ResponseSHA256+" "+outcome+" "+reason)
	return nil
}

func TestPassOpensAChangeRequestAndBlockedDoesNot(t *testing.T) {
	open := &memoryOpen{}
	audit := &memoryAudit{}
	service := Service{
		Proposer: recorded.Fixed(domain.Proposal{Field: "memory", Value: "512Mi", Rationale: "headroom"}),
		Open:     open, Audit: audit,
	}
	input := domain.PackagingContext{RunID: "sample-app-1", GateRules: "memory limit must not exceed 1Gi"}
	passed, err := service.Propose(context.Background(), input, "dev", "operator")
	if err != nil || passed.Outcome != "PASS" || passed.ChangeRequestID != "crq_test" || passed.Label != "Live model call" {
		t.Fatalf("pass = %#v %v", passed, err)
	}
	service.Proposer = recorded.Fixed(domain.Proposal{Field: "memory", Value: "8Gi", Rationale: "unbounded"})
	blocked, err := service.Propose(context.Background(), input, "dev", "operator")
	if err != nil || blocked.Outcome != "FAIL_REQUIRED" || blocked.ChangeRequestID != "" || len(open.ids) != 1 {
		t.Fatalf("blocked = %#v open=%v %v", blocked, open.ids, err)
	}
	if len(audit.rows) != 2 || audit.rows[0] == "" {
		t.Fatalf("audit = %#v", audit.rows)
	}
}

func TestUnavailableLeavesNoChangeRequest(t *testing.T) {
	open := &memoryOpen{}
	result, err := (Service{Proposer: recorded.Unavailable(), Open: open}).Propose(
		context.Background(), domain.PackagingContext{RunID: "sample-app-1"}, "dev", "operator",
	)
	if err != nil || result.Error == "" || len(open.ids) != 0 {
		t.Fatalf("unavailable = %#v %v", result, err)
	}
}
