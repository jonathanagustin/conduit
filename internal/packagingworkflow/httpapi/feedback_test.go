package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

type feedbackRuns struct{ status domain.RunStatus }

func (runs feedbackRuns) Read(_ context.Context, runID string) (domain.RunStatus, error) {
	if runID != runs.status.RunID {
		return domain.RunStatus{}, ErrNotFound
	}
	return runs.status, nil
}

func (runs feedbackRuns) List(context.Context) ([]domain.RunStatus, error) {
	return []domain.RunStatus{runs.status}, nil
}

func (runs feedbackRuns) Events(context.Context, string) ([]domain.StepEvent, error) {
	return nil, nil
}

func TestFeedbackIsReviewInputAndNotApproval(t *testing.T) {
	store := NewMemoryFeedback()
	mux := http.NewServeMux()
	Register(mux, Config{
		Reader: feedbackRuns{status: domain.RunStatus{RunID: "sample-app-1", Phase: domain.PhaseReview}},
		Authorize: func(*http.Request) error {
			return nil
		},
		Feedback: store,
		OperatorName: func(*http.Request) string {
			return "reviewer"
		},
	})
	body := `{"target":"step","selector":"sign","note":"the digest line is hard to read"}`
	created := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/sample-app-1/feedback", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	var note FeedbackNote
	if err := json.Unmarshal(created.Body.Bytes(), &note); err != nil {
		t.Fatal(err)
	}
	if note.Approval || note.Author != "reviewer" || note.Target != "step" || note.Selector != "sign" {
		t.Fatalf("note = %#v", note)
	}
	listed := httptest.NewRecorder()
	mux.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/v1/packaging-runs/sample-app-1/feedback", nil))
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"approval":false`) {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}
	rejected := httptest.NewRecorder()
	mux.ServeHTTP(rejected, httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/sample-app-1/feedback", strings.NewReader(`{"target":"approval","selector":"plan","note":"ship it"}`)))
	if rejected.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad target = %d %s", rejected.Code, rejected.Body.String())
	}
	anonymous := http.NewServeMux()
	Register(anonymous, Config{Reader: feedbackRuns{status: domain.RunStatus{RunID: "sample-app-1"}}})
	refused := httptest.NewRecorder()
	anonymous.ServeHTTP(refused, httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/sample-app-1/feedback", strings.NewReader(body)))
	if refused.Code != http.StatusUnauthorized {
		bodyBytes, _ := io.ReadAll(refused.Body)
		t.Fatalf("anonymous = %d %s", refused.Code, bodyBytes)
	}
}
