package application

import (
	"context"
	"fmt"
	"testing"
)

type memoryOpen struct{ calls int }

func (open *memoryOpen) Open(context.Context, string, string, string, string, string, string) (string, string, error) {
	open.calls++
	return "crq_eval", "PENDING_APPROVALS", nil
}

type memoryAudit struct{ n int }

func (audit *memoryAudit) Record(context.Context, string, string, string, string, string) (string, error) {
	audit.n++
	return "aud_eval", nil
}

func TestRecordedEvalPassesTheCeilingAndBlocksEightGi(t *testing.T) {
	service := New("recorded", "45s")
	service.Audit = &memoryAudit{}
	results, auditID, err := service.Run(context.Background(), "local-developer")
	if err != nil || auditID == "" {
		t.Fatal(err)
	}
	byID := map[string]CaseResult{}
	for _, result := range results {
		byID[result.Case.ID] = result
		if result.Columns[0].Label != "No model call" || result.Columns[0].Adapter != "recorded" {
			t.Fatalf("column = %#v", result.Columns[0])
		}
	}
	if !byID["mem-512"].Columns[0].Passed || byID["mem-512"].Columns[0].Outcome != "PASS" {
		t.Fatalf("512 = %#v", byID["mem-512"])
	}
	if !byID["mem-8gi"].Columns[0].Passed || byID["mem-8gi"].Columns[0].Outcome != "FAIL_REQUIRED" {
		t.Fatalf("8Gi = %#v", byID["mem-8gi"])
	}
}

func TestSelectingACourseOpensAPendingRequest(t *testing.T) {
	open := &memoryOpen{}
	service := New("", "")
	service.Opener = open
	service.Audit = &memoryAudit{}
	id, status, err := service.Select(context.Background(), "local-developer", "512Mi", "dev", "sample-app", "app.example:sample-app")
	if err != nil || id != "crq_eval" || status != "PENDING_APPROVALS" || open.calls != 1 {
		t.Fatalf("select = %s %s calls=%d %v", id, status, open.calls, err)
	}
}

func TestFeedbackBecomesACase(t *testing.T) {
	service := New("recorded", "45s")
	added, err := service.AddCase(context.Background(), "operator rejected 2Gi", "sample-app-1", "2Gi", "FAIL_REQUIRED")
	if err != nil || added.Source != "operator feedback" {
		t.Fatalf("added = %#v %v", added, err)
	}
	if len(mustCases(t, service)) != 4 {
		t.Fatalf("cases = %d", len(mustCases(t, service)))
	}
}

type memoryProposer struct {
	calls []string
	value string
	err   error
}

func (proposer *memoryProposer) Propose(_ context.Context, scenario, value string) (string, error) {
	proposer.calls = append(proposer.calls, scenario+"|"+value)
	return proposer.value, proposer.err
}

func TestLiveEvalRunsOnlyTheCeilingPassCase(t *testing.T) {
	proposer := &memoryProposer{value: "256Mi"}
	service := New("codex", "45s")
	service.Proposer = proposer
	results, _, err := service.Run(context.Background(), "local-developer")
	if err != nil {
		t.Fatal(err)
	}
	if len(proposer.calls) != 1 || proposer.calls[0] != "A 512Mi limit is within the ceiling|512Mi" {
		t.Fatalf("calls = %#v", proposer.calls)
	}
	byID := map[string]CaseResult{}
	for _, result := range results {
		byID[result.Case.ID] = result
	}
	if len(byID["mem-512"].Columns) != 2 || byID["mem-512"].Columns[1].Label != "Live model call" || byID["mem-512"].Columns[1].Adapter != "codex" {
		t.Fatalf("live 512 = %#v", byID["mem-512"].Columns)
	}
	if !byID["mem-512"].Columns[1].Passed || byID["mem-512"].Columns[1].Outcome != "PASS" {
		t.Fatalf("live 512 score = %#v", byID["mem-512"].Columns[1])
	}
	if len(byID["mem-8gi"].Columns) != 1 || byID["mem-8gi"].Columns[0].Label != "No model call" {
		t.Fatalf("8Gi should stay recorded-only: %#v", byID["mem-8gi"].Columns)
	}
}

func TestBlockedCourseNeverCallsProposalOpener(t *testing.T) {
	open := &memoryOpen{}
	service := New("", "")
	service.Opener = open
	for _, value := range []string{"8Gi", "", "invalid"} {
		if _, _, err := service.Select(context.Background(), "writer", value, "env-a", "api", "product-a"); err != ErrCourseBlocked {
			t.Fatal(value, err)
		}
	}
	if open.calls != 0 {
		t.Fatal("blocked course opened a request", open.calls)
	}
}

func TestFeedbackIdentityCoversExpectedScopeAndBoundsGrowth(t *testing.T) {
	service := New("", "")
	first, err := service.AddCase(context.Background(), "same scenario", "one", "512Mi", "PASS")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := service.AddCase(context.Background(), "same scenario", "one", "512Mi", "PASS")
	if err != nil || retry.ID != first.ID || len(mustCases(t, service)) != 4 {
		t.Fatal("feedback retry duplicated case", retry, err)
	}
	other, err := service.AddCase(context.Background(), "same scenario", "two", "512Mi", "FAIL_REQUIRED")
	if err != nil || other.ID == first.ID {
		t.Fatal("feedback identity lost bound/expectation", other, err)
	}
	for i := len(mustCases(t, service)); i < 256; i++ {
		if _, err := service.AddCase(context.Background(), fmt.Sprint(i), "scope", "512Mi", "PASS"); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err := service.AddCase(context.Background(), "overflow", "scope", "512Mi", "PASS"); err == nil {
		t.Fatal("unbounded evaluation cases accepted")
	}
	if _, err := service.AddCase(context.Background(), "same scenario", "one", "512Mi", "PASS"); err != nil {
		t.Fatal("bounded store refused idempotent retry", err)
	}
}
