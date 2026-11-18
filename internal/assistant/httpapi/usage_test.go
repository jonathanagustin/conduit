package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

type reportedUsageModel struct{ *scripted }

func (model reportedUsageModel) Complete(ctx context.Context, prompt string) (string, domain.Provenance, error) {
	raw, provenance, err := model.scripted.Complete(ctx, prompt)
	input, output := int64(2), int64(0)
	provenance.Usage = &domain.ReportedTokenUsage{PromptTokens: &input, CompletionTokens: &output}
	return raw, provenance, err
}

func TestFailedStreamReportsUsageWithoutDeclaringSuccess(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/assistant/turns", strings.NewReader(`{"mode":"explore","message":"loop"}`))
	request.Header.Set("X-Test-User", "ada")
	request.Header.Set("Accept", "text/event-stream")
	response := httptest.NewRecorder()
	handler(reportedUsageModel{&scripted{replies: []string{askForEnvironments}}}).ServeHTTP(response, request)
	list := events(t, response.Body) // Includes the pinned AG-UI sequence validator.
	if response.Code != http.StatusOK || len(list) < 2 {
		t.Fatalf("status=%d events=%v", response.Code, list)
	}
	snapshot, last := list[len(list)-2], list[len(list)-1]
	if snapshot["type"] != "CUSTOM" || snapshot["name"] != "conduit.assistant.turn-result" || last["type"] != "RUN_ERROR" || last["code"] != "ROUND_CAP" {
		t.Fatalf("wrong terminal sequence: %v %v", snapshot, last)
	}
	result := snapshot["value"].(map[string]any)
	usage := result["usage"].(map[string]any)
	if usage["promptTokens"] != float64(2*domain.MaxRounds) || usage["completionTokens"] != float64(0) {
		t.Fatalf("partial result usage=%v", usage)
	}
	for _, event := range list {
		if event["type"] == "RUN_FINISHED" {
			t.Fatal("failed turn declared success")
		}
	}
}
