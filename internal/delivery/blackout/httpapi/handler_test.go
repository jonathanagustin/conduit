package httpapi

import (
	"encoding/json"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/blackout/application"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnonymousBlackoutReadIsRefused(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return http.ErrNoCookie }})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/blackouts?environment=east", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestReadReturnsTheStoredReason(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{
		Authorize: func(*http.Request) error { return nil },
		Active: func(environment string, at time.Time) string {
			if environment == "east" {
				return "change freeze for the rehearsal"
			}
			return ""
		},
	})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/blackouts?environment=east", nil))
	if response.Code != http.StatusOK || response.Body.String() == "" {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
}

func TestOptInSnapshotPreservesDefaultResponseAndExplicitEvaluationTime(t *testing.T) {
	service := &application.Service{Store: controlplane.NewMemoryRepository()}
	at := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	result, err := service.Declare(t.Context(), "ada", "east", "reviewed freeze", at, at.Add(time.Hour), true)
	if err != nil || !result.Allowed {
		t.Fatal("declare", err)
	}
	mux := http.NewServeMux()
	Register(mux, Config{Active: service.Active, Snapshot: service.Snapshot})
	for _, query := range []string{"", "&includeIntervals=false", "&includeIntervals=true"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/blackouts?environment=east&at=2026-11-18T12:00:00Z"+query, nil))
		var body map[string]json.RawMessage
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil {
			t.Fatal(response.Code, response.Body.String())
		}
		if string(body["blocked"]) != "true" || string(body["reason"]) != `"reviewed freeze"` {
			t.Fatal("lost evaluated decision", response.Body.String())
		}
		if query != "&includeIntervals=true" {
			if len(body) != 3 {
				t.Fatal("changed legacy response", response.Body.String())
			}
			continue
		}
		var snapshot application.Snapshot
		if json.Unmarshal(response.Body.Bytes(), &snapshot) != nil || len(snapshot.Intervals) != 1 || snapshot.Intervals[0].Author != "ada" || snapshot.Intervals[0].Environment != "east" || !snapshot.EvaluatedAt.Equal(at) || snapshot.Truncated {
			t.Fatal("snapshot", response.Body.String())
		}
	}
}

func TestIntervalSnapshotRefusesUnavailableOrMalformedRequests(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{})
	for query, status := range map[string]int{"includeIntervals=true": 501, "includeIntervals=maybe": 400, "includeIntervals=true&at=invalid": 400} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/blackouts?environment=east&"+query, nil))
		if response.Code != status {
			t.Fatal(query, response.Code)
		}
	}
}
