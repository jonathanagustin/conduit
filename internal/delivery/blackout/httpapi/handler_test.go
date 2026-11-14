package httpapi

import (
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
