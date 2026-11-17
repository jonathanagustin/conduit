package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonathanagustin/conduit/internal/modeleval"
	"github.com/jonathanagustin/conduit/internal/testsupport/modelhttpfixture"
)

// This records the canned utility through the supported HTTP port. It is
// deterministic transport/digest evidence, never a real-model evaluation.
func TestDeterministicHTTPModelEvaluationFixture(t *testing.T) {
	corpus, err := modeleval.LoadCorpus("../../docs/evidence/model-evals")
	if err != nil {
		t.Fatal(err)
	}
	modelhttpfixture.Configure(t, runAgentExecPrompt)
	provider, err := modeleval.ProviderFromEnv(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if provider.ID != corpus.GateModel {
		t.Fatalf("fixture provider %s differs from corpus gate %s", provider.ID, corpus.GateModel)
	}
	outcome, err := modeleval.Run(t.Context(), corpus, modeleval.Options{Mode: modeleval.ModeRecord, ModelID: provider.ID, Live: provider.Model})
	if err != nil {
		t.Fatal(err)
	}
	baselinePath := modeleval.BaselinePath(corpus.Dir, provider.ID)
	if os.Getenv("CONDUIT_RECORD_EVAL_FIXTURE") == "1" {
		historical, err := modeleval.LoadReport(filepath.Join(corpus.Dir, "baselines/agent-exec-deterministic.json"))
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Report.Aggregate != historical.Aggregate || len(modeleval.Regressions(outcome.Report, historical)) != 0 {
			t.Fatalf("fixture refresh changed expected scoring: old=%+v new=%+v regressions=%v", historical.Aggregate, outcome.Report.Aggregate, modeleval.Regressions(outcome.Report, historical))
		}
		if err := modeleval.WriteTranscripts(modeleval.TranscriptDir(corpus.Dir, provider.ID), outcome.Transcripts); err != nil {
			t.Fatal(err)
		}
		if err := modeleval.WriteReport(baselinePath, outcome.Report); err != nil {
			t.Fatal(err)
		}
		t.Logf("Recorded %d canned HTTP exchanges; preserved %d/%d score and every previously passing assertion", len(outcome.Transcripts), outcome.Report.Aggregate.Passed, outcome.Report.Aggregate.Total)
		return
	}
	baseline, err := modeleval.LoadReport(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(outcome.Report)
	want, _ := json.Marshal(baseline)
	if string(got) != string(want) {
		t.Fatal("fresh deterministic HTTP fixture differs from committed evidence; review and record with CONDUIT_RECORD_EVAL_FIXTURE=1")
	}
}
