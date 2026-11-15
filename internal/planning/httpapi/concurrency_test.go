package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func TestBusySessionIsAConflict(t *testing.T) {
	response := httptest.NewRecorder()
	writeDomainError(response, domain.ErrTurnInProgress)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d", response.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != domain.CodeTurnInProgress {
		t.Fatalf("body=%v", body)
	}
}
