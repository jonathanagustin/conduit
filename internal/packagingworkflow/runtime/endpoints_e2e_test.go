package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/planlifecycle"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/config"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/pipeline"
)

// TestPackagingEndpoints exercises list, read, events, cancel, and rerun
// against a Temporal frontend and a PostgreSQL repository. The shell gate
// sets PACKAGING_E2E; an ordinary go test leaves it unset and skips.
func TestPackagingEndpoints(t *testing.T) {
	if os.Getenv("PACKAGING_E2E") != "1" {
		t.Skip("PACKAGING_E2E is unset")
	}
	address := os.Getenv("PACKAGING_TEMPORAL_ADDRESS")
	dsn := os.Getenv("POSTGRES_DSN")
	if address == "" {
		t.Fatal("PACKAGING_TEMPORAL_ADDRESS is required")
	}
	if dsn == "" {
		t.Fatal("POSTGRES_DSN is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	repository, err := controlplane.NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatalf("PostgreSQL: %v", err)
	}
	defer repository.Close()

	temporalClient, err := Dial(config.Config{Enabled: true, Address: address, Namespace: "default"})
	if err != nil {
		t.Fatalf("Temporal: %v", err)
	}
	defer temporalClient.Close()

	dir := t.TempDir()
	fixtureDir := filepath.Join(dir, "fixture")
	if err := fixtures.Materialize(fixtureDir); err != nil {
		t.Fatal(err)
	}
	ledger, err := adapters.Open(filepath.Join(dir, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	work := pipeline.RegisterWorker(temporalClient, pipeline.TaskQueue, pipeline.Activities{
		FixtureDir: fixtureDir,
		Ledger:     ledger,
		Plans:      planlifecycle.New(repository, ledger),
	})
	if err := work.Start(); err != nil {
		t.Fatal(err)
	}
	defer work.Stop()

	identity, err := fixtures.Identity("sample-app-e2e")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: identity.WorkflowID(), TaskQueue: pipeline.TaskQueue,
	}, pipeline.WorkflowName, pipeline.Input{Identity: identity, ApprovalWait: 15 * time.Minute}); err != nil {
		t.Fatal(err)
	}

	reader := StatusReader(temporalClient, pipeline.TaskQueue)
	var status domain.RunStatus
	deadline := time.Now().Add(90 * time.Second)
	for {
		status, err = reader.Read(ctx, identity.RunID)
		if err == nil && status.Phase == domain.PhaseReview {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("packaging run did not reach review: status=%#v err=%v", status, err)
		}
		time.Sleep(time.Second)
	}

	decisions := &memoryDecisions{}
	mux := http.NewServeMux()
	packaginghttp.Register(mux, packaginghttp.Config{
		Reader:    reader,
		Controls:  reader,
		Decisions: decisions,
		Authorize: func(*http.Request) error { return nil },
		Visible:   func(*http.Request, string) bool { return true },
		Permit: func(*http.Request, string) (packaginghttp.Operator, error) {
			return packaginghttp.Operator{Username: "operator"}, nil
		},
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	list := getJSON(t, server.URL+"/v1/packaging-runs")
	items, _ := list["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("list = %#v", list)
	}
	read := getJSON(t, server.URL+"/v1/packaging-runs/"+identity.RunID)
	if read["runId"] != identity.RunID || read["phase"] != domain.PhaseReview {
		t.Fatalf("read = %#v", read)
	}
	events := getJSON(t, server.URL+"/v1/packaging-runs/"+identity.RunID+"/events")
	eventItems, _ := events["items"].([]any)
	if len(eventItems) == 0 {
		t.Fatalf("events = %#v", events)
	}

	binding := map[string]string{
		"decisionId":       "d-cancel",
		"sourceDigest":     status.SourceDigest,
		"proposalRevision": status.ProposalRevision,
		"targetPlanRef":    status.TargetPlanRef,
	}
	cancelled := postJSON(t, server.URL+"/v1/packaging-runs/"+identity.RunID+"/cancel", binding, http.StatusOK)
	if cancelled["runId"] != identity.RunID {
		t.Fatalf("cancel = %#v", cancelled)
	}
	binding["decisionId"] = "d-rerun"
	binding["templateVersion"] = identity.TemplateVersion
	binding["toolVersions"] = identity.ToolVersions
	rerun := postJSON(t, server.URL+"/v1/packaging-runs/"+identity.RunID+"/rerun", binding, http.StatusCreated)
	if rerun["runId"] == "" || rerun["runId"] == identity.RunID {
		t.Fatalf("rerun = %#v", rerun)
	}
}

type memoryDecisions struct {
	rows []packaginghttp.DecisionRecord
}

func (log *memoryDecisions) Lookup(_ context.Context, runID, decisionID string) (packaginghttp.DecisionRecord, bool, error) {
	for _, row := range log.rows {
		if row.RunID == runID && row.DecisionID == decisionID {
			return row, true, nil
		}
	}
	return packaginghttp.DecisionRecord{}, false, nil
}

func (log *memoryDecisions) Append(_ context.Context, record packaginghttp.DecisionRecord) error {
	log.rows = append(log.rows, record)
	return nil
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", url, response.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func postJSON(t *testing.T, url string, payload map[string]string, want int) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(url, "application/json", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("POST %s = %d %#v", url, response.StatusCode, body)
	}
	return body
}
