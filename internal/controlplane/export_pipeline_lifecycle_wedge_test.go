package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A Change Request id is a digest of TargetKind and TargetID alone, and the
// create guard hands back whatever record already occupies that id whatever its
// status. Proposing an Export Pipeline lifecycle change against a bare pipeline
// id therefore lets the first archive of a pipeline own that id for good: once
// it reaches a terminal status the pipeline can never be archived again, and
// Reopen is author-gated, so a departed author leaves no recourse at all.
func TestATerminalPipelineLifecycleRequestDoesNotWedgeThePipeline(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BundlePrivateKey:                     mustBundleKey(t),
	})
	if _, err := NewGovernanceService(repository).PutPolicy(ctx, ApprovalPolicy{
		ID: "pipeline-lifecycle", DisplayName: "Pipeline lifecycle", Enabled: true,
		Expression: `type.endsWith("_PIPELINE") ? approve("fixture approval") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}

	definition, err := json.Marshal(ExportPipeline{
		ID: "wedged-export", DisplayName: "Wedged export", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	put := httptest.NewRequest(http.MethodPut, "/v1/export-pipelines/wedged-export", bytes.NewReader(definition))
	put.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, put)
	if response.Code != http.StatusOK {
		t.Fatalf("put pipeline = %d %s", response.Code, response.Body.String())
	}

	lifecycle := func(method, path string) ChangeRequest {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		if recorder.Code != http.StatusCreated {
			t.Fatalf("%s %s = %d %s", method, path, recorder.Code, recorder.Body.String())
		}
		var result struct {
			ChangeRequest ChangeRequest `json:"changeRequest"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("%s %s response = %v", method, path, err)
		}
		return result.ChangeRequest
	}
	status := func() string {
		t.Helper()
		pipeline, err := repository.GetExportPipeline(ctx, "wedged-export")
		if err != nil {
			t.Fatal(err)
		}
		return pipeline.Status
	}

	archived := lifecycle(http.MethodPost, "/v1/export-pipelines/wedged-export/archive")
	if status() != ExportPipelineArchived {
		t.Fatalf("the first archive left the pipeline %s", status())
	}

	// The author withdraws that Change Request, which is terminal: Cancel admits
	// only PENDING_APPROVALS and APPROVED, and Reopen answers only to the author.
	cancel := httptest.NewRequest(http.MethodPost, "/v1/change-requests/"+archived.ID+"/cancel",
		strings.NewReader(`{"reason":"withdrawn"}`))
	cancel.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, cancel)
	if response.Code != http.StatusOK {
		t.Fatalf("cancel = %d %s", response.Code, response.Body.String())
	}
	cancelled, err := repository.GetChangeRequest(ctx, archived.ID)
	if err != nil || cancelled.Status != ChangeCancelled {
		t.Fatalf("cancelled request = %#v, %v", cancelled, err)
	}

	// The pipeline goes back into service, and then has to be archivable again.
	// This is a different change from the withdrawn one -- it acts on a pipeline
	// that has moved twice since -- so it must get its own Change Request rather
	// than the terminal record that happens to share its target.
	lifecycle(http.MethodPost, "/v1/export-pipelines/wedged-export/unarchive")
	if status() != ExportPipelineActive {
		t.Fatalf("unarchive left the pipeline %s", status())
	}
	again := lifecycle(http.MethodPost, "/v1/export-pipelines/wedged-export/archive")
	if again.ID == archived.ID {
		t.Errorf("the second archive was answered with the withdrawn request %s (%s)", again.ID, again.Status)
	}
	if again.Status != ChangeApproved {
		t.Errorf("the second archive was evaluated as %s, want %s", again.Status, ChangeApproved)
	}
	if status() != ExportPipelineArchived {
		t.Fatalf("a terminal Change Request wedged the pipeline: it is %s after a second archive", status())
	}
}
