package application

import (
	"context"
	"errors"
	"strings"
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

type recordedFacts struct{ facts domain.PackagingContext }

func (facts recordedFacts) Load(context.Context, string) (domain.PackagingContext, error) {
	return facts.facts, nil
}

type captureProposer struct{ got domain.PackagingContext }

func (capture *captureProposer) Propose(_ context.Context, input domain.PackagingContext) (domain.Proposal, domain.Provenance, error) {
	capture.got = input
	return domain.Proposal{Field: "memory", Value: "512Mi", Rationale: "the chart memory is 512Mi and the current limit is 512Mi"}, domain.Provenance{Adapter: "capture"}, nil
}

func TestRecordedFactsReachTheProposer(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ab", 32)
	capture := &captureProposer{}
	service := Service{
		Proposer: capture,
		Facts: recordedFacts{facts: domain.PackagingContext{
			ChartValues: "memory: 512Mi", Resources: "limits.memory=512Mi requests.memory=256Mi",
			MemoryRequest: "256Mi", MemoryLimit: "512Mi",
			ImageRef: "127.0.0.1:5000/sample-app:0.2.0", ImageDigest: digest,
		}},
		Open: &memoryOpen{}, Audit: &memoryAudit{},
	}
	_, err := service.Propose(context.Background(), domain.PackagingContext{
		RunID: "sample-app-1", GateRules: domain.MemoryGateRule,
	}, "dev", "operator")
	if err != nil {
		t.Fatal(err)
	}
	got := capture.got
	if got.ChartValues != "memory: 512Mi" || got.Resources != "limits.memory=512Mi requests.memory=256Mi" ||
		got.MemoryRequest != "256Mi" || got.MemoryLimit != "512Mi" ||
		got.ImageRef != "127.0.0.1:5000/sample-app:0.2.0" || got.ImageDigest != digest ||
		got.GateRules != domain.MemoryGateRule {
		t.Fatalf("adapter input = %#v", got)
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
