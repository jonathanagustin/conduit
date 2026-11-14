package application

import (
	"context"
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
	added, err := service.AddCase("operator rejected 2Gi", "sample-app-1", "2Gi", "FAIL_REQUIRED")
	if err != nil || added.Source != "operator feedback" {
		t.Fatalf("added = %#v %v", added, err)
	}
	if len(service.Cases()) != 4 {
		t.Fatalf("cases = %d", len(service.Cases()))
	}
}
