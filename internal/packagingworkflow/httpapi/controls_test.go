package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type memoryDecisions struct{ rows []DecisionRecord }

func (log *memoryDecisions) Lookup(_ context.Context, runID, decisionID string) (DecisionRecord, bool, error) {
	for _, row := range log.rows {
		if row.RunID == runID && row.DecisionID == decisionID {
			return row, true, nil
		}
	}
	return DecisionRecord{}, false, nil
}

func (log *memoryDecisions) Append(_ context.Context, record DecisionRecord) error {
	log.rows = append(log.rows, record)
	return nil
}

type liveRuns struct {
	runs       []domain.RunStatus
	cancels    int
	started    []domain.RunIdentity
	configured bool
}

func (runs *liveRuns) Configured() bool { return runs.configured }

func (runs *liveRuns) Read(_ context.Context, runID string) (domain.RunStatus, error) {
	for _, run := range runs.runs {
		if run.RunID == runID {
			return run, nil
		}
	}
	return domain.RunStatus{}, ErrNotFound
}

func (runs *liveRuns) List(context.Context) ([]domain.RunStatus, error) {
	return append([]domain.RunStatus(nil), runs.runs...), nil
}

func (runs *liveRuns) Events(context.Context, string) ([]domain.StepEvent, error) {
	return nil, ErrNotFound
}

func (runs *liveRuns) Cancel(_ context.Context, runID string) error {
	runs.cancels++
	for i := range runs.runs {
		if runs.runs[i].RunID != runID {
			continue
		}
		runs.runs[i].Phase = domain.PhaseBlocked
		runs.runs[i].Outcome = string(domain.OutcomeCancelled)
		return nil
	}
	return ErrNotFound
}

func (runs *liveRuns) Start(_ context.Context, id domain.RunIdentity) (domain.RunStatus, error) {
	runs.started = append(runs.started, id)
	status := domain.RunStatus{
		RunID: id.RunID, Phase: domain.PhaseAssess, SourceDigest: id.SourceDigest, TargetPlanRef: id.TargetPlanRef,
	}
	runs.runs = append(runs.runs, status)
	return status, nil
}

func permitEnvA(request *http.Request, environment string) (Operator, error) {
	if environment != "env-a" {
		return Operator{}, StatusError{Status: http.StatusForbidden, Message: "forbidden"}
	}
	username := request.Header.Get("X-User")
	if username == "" {
		username = "alice"
	}
	return Operator{Username: username}, nil
}

func decisionBody(id, digest, revision string) string {
	return fmt.Sprintf(
		`{"decisionId":%q,"sourceDigest":%q,"proposalRevision":%q,"targetPlanRef":"env-a/app","templateVersion":"fixture-1","toolVersions":"lint=fixture-1"}`,
		id, digest, revision,
	)
}

func postControl(mux *http.ServeMux, target, user, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	request.Header.Set("X-User", user)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func TestCancelAndRerunFailClosedUntilGoverned(t *testing.T) {
	runs := &liveRuns{
		configured: true,
		runs: []domain.RunStatus{
			{RunID: "run-review", Phase: domain.PhaseReview, SourceDigest: "sha256:abc", ProposalRevision: "rev-1", TargetPlanRef: "env-a/app"},
			{RunID: "run-done", Phase: domain.PhaseComplete, Outcome: string(domain.OutcomeHandoffRecorded), SourceDigest: "sha256:abc", ProposalRevision: "rev-9", TargetPlanRef: "env-a/app", PlanID: "plan_done"},
		},
	}
	log := &memoryDecisions{}
	mux := mount(t, Config{Authorize: authed(), Permit: permitEnvA, Reader: runs, Controls: runs, Decisions: log})

	anonymous := mount(t, Config{Authorize: func(*http.Request) error {
		return StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
	}})
	if got := postControl(anonymous, "/v1/packaging-runs/run-review/cancel", "", decisionBody("d-anon", "sha256:abc", "rev-1")); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", got.Code)
	}

	unconfigured := mount(t, Config{Authorize: authed(), Permit: permitEnvA, Decisions: log})
	if got := postControl(unconfigured, "/v1/packaging-runs/run-review/cancel", "alice", decisionBody("d-off", "sha256:abc", "rev-1")); got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "not configured") || runs.cancels != 0 {
		t.Fatalf("unconfigured = %d %s cancels=%d", got.Code, got.Body.String(), runs.cancels)
	}

	body := decisionBody("d-cancel", "sha256:abc", "rev-1")
	cancelled := postControl(mux, "/v1/packaging-runs/run-review/cancel", "alice", body)
	if cancelled.Code != http.StatusOK || !strings.Contains(cancelled.Body.String(), `"outcome":"CANCELLED"`) || !strings.Contains(cancelled.Body.String(), `"planCancelled":false`) || runs.cancels != 1 {
		t.Fatalf("cancel = %d %s cancels=%d", cancelled.Code, cancelled.Body.String(), runs.cancels)
	}
	status := get(mux, "/v1/packaging-runs/run-review")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"outcome":"CANCELLED"`) {
		t.Fatalf("status after cancel = %d %s", status.Code, status.Body.String())
	}
	if again := postControl(mux, "/v1/packaging-runs/run-review/cancel", "alice", body); again.Code != http.StatusConflict || runs.cancels != 1 {
		t.Fatalf("duplicate = %d cancels=%d", again.Code, runs.cancels)
	}
	if other := postControl(mux, "/v1/packaging-runs/run-review/cancel", "bob", body); other.Code != http.StatusForbidden || runs.cancels != 1 {
		t.Fatalf("cross-user = %d", other.Code)
	}
	if stale := postControl(mux, "/v1/packaging-runs/run-review/cancel", "alice", decisionBody("d-stale", "sha256:other", "rev-1")); stale.Code != http.StatusConflict || runs.cancels != 1 {
		t.Fatalf("stale = %d %s", stale.Code, stale.Body.String())
	}
	handed := postControl(mux, "/v1/packaging-runs/run-done/cancel", "alice", decisionBody("d-plan", "sha256:abc", "rev-9"))
	if handed.Code != http.StatusConflict || !strings.Contains(handed.Body.String(), `"planId":"plan_done"`) || !strings.Contains(handed.Body.String(), "/v1/plans/plan_done/cancel") || !strings.Contains(handed.Body.String(), `"planCancelled":false`) || runs.cancels != 1 {
		t.Fatalf("handed off = %d %s cancels=%d", handed.Code, handed.Body.String(), runs.cancels)
	}

	rerun := postControl(mux, "/v1/packaging-runs/run-done/rerun", "alice", decisionBody("d-rerun", "sha256:abc", "rev-9"))
	if rerun.Code != http.StatusCreated {
		t.Fatalf("rerun = %d %s", rerun.Code, rerun.Body.String())
	}
	var started controlResult
	if err := json.Unmarshal(rerun.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.RunID == "" || started.RunID == "run-done" || started.ProposalRevision != "" || started.PriorRunID != "run-done" || started.PlanCancelled {
		t.Fatalf("started = %#v", started)
	}
	if len(runs.started) != 1 || runs.started[0].ProposalRevision != "" || runs.started[0].RunID != started.RunID || runs.started[0].SourceDigest != "sha256:abc" {
		t.Fatalf("identity = %#v", runs.started)
	}
	if again := postControl(mux, "/v1/packaging-runs/run-done/rerun", "alice", decisionBody("d-rerun", "sha256:abc", "rev-9")); again.Code != http.StatusConflict || len(runs.started) != 1 {
		t.Fatalf("rerun duplicate = %d starts=%d", again.Code, len(runs.started))
	}

	var allowed DecisionRecord
	for _, row := range log.rows {
		if row.DecisionID == "d-cancel" && row.Verdict == verdictAllow {
			allowed = row
		}
	}
	if allowed.Subject != "alice" || allowed.SourceDigest != "sha256:abc" || allowed.ProposalRevision != "rev-1" || allowed.At.IsZero() {
		t.Fatalf("audit = %#v", allowed)
	}
}

func TestRerunDoesNotReuseAnEarlierApproval(t *testing.T) {
	runs := &liveRuns{configured: true, runs: []domain.RunStatus{{
		RunID: "run-old", Phase: domain.PhaseComplete, SourceDigest: "sha256:new", ProposalRevision: "rev-old",
		TargetPlanRef: "env-a/app", PlanID: "plan_old",
	}}}
	mux := mount(t, Config{Authorize: authed(), Permit: permitEnvA, Reader: runs, Controls: runs, Decisions: &memoryDecisions{}})
	response := postControl(mux, "/v1/packaging-runs/run-old/rerun", "alice", decisionBody("d-fresh", "sha256:new", "rev-old"))
	if response.Code != http.StatusCreated || runs.started[0].ProposalRevision != "" || strings.Contains(response.Body.String(), "rev-old") {
		t.Fatalf("rerun inherited a revision: %d %s identity=%#v", response.Code, response.Body.String(), runs.started)
	}
}
