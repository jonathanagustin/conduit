package composition

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestAssistantThreadsBelongToTheOperatorWhoOpenedThem(t *testing.T) {
	handler, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	as := func(subject, method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if subject != "" {
			request.Header.Set("X-Platform-Subject", subject)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	created := as("ada", http.MethodPost, "/v1/assistant/threads", `{"title":"ada's private thread"}`)
	var thread struct {
		ID string `json:"id"`
	}
	if created.Code != http.StatusCreated || json.Unmarshal(created.Body.Bytes(), &thread) != nil || thread.ID == "" {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	if own := as("ada", http.MethodGet, "/v1/assistant/threads/"+thread.ID, ""); own.Code != http.StatusOK || !strings.Contains(own.Body.String(), "private thread") {
		t.Fatalf("owner read = %d %s", own.Code, own.Body.String())
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		foreign := as("grace", method, "/v1/assistant/threads/"+thread.ID, "")
		if foreign.Code != http.StatusNotFound || strings.Contains(foreign.Body.String(), "private") {
			t.Fatalf("foreign %s = %d %s", method, foreign.Code, foreign.Body.String())
		}
	}
	if listed := as("grace", http.MethodGet, "/v1/assistant/threads", ""); listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), thread.ID) {
		t.Fatalf("foreign list = %d %s", listed.Code, listed.Body.String())
	}
	// With insecure identity headers a request naming nobody is the default
	// development principal, which is still not ada. The refusal of a caller
	// with no credential at all is TestEveryRegisteredRouteRefusesAnAnonymousCaller.
	if fallback := as("", http.MethodGet, "/v1/assistant/threads/"+thread.ID, ""); fallback.Code != http.StatusNotFound {
		t.Fatalf("default principal = %d %s", fallback.Code, fallback.Body.String())
	}
	// No model is configured here: the turn is refused with the code, and with
	// the label the console already keys on.
	turned := as("ada", http.MethodPost, "/v1/assistant/turns", `{"mode":"explore","message":"hi","threadId":"`+thread.ID+`"}`)
	if turned.Code != http.StatusUnprocessableEntity || !strings.Contains(turned.Body.String(), `"code":"MODEL_NOT_CONFIGURED"`) || !strings.Contains(turned.Body.String(), "No model call") {
		t.Fatalf("turn = %d %s", turned.Code, turned.Body.String())
	}
	if gone := as("ada", http.MethodDelete, "/v1/assistant/threads/"+thread.ID, ""); gone.Code != http.StatusNoContent {
		t.Fatalf("owner delete = %d %s", gone.Code, gone.Body.String())
	}
}
