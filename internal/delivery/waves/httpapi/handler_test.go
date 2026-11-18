package httpapi

import (
	"errors"
	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRolloutListRefusesBeforeEvidenceAndRedactsBackendFailures(t *testing.T) {
	for _, tc := range []struct {
		authorize func(*http.Request) error
		status    int
		read      bool
	}{{nil, 401, false}, {func(*http.Request) error { return errors.New("private identity diagnostic") }, 401, false}, {func(*http.Request) error { return StatusError{403, "operator delegation refused"} }, 403, false}, {func(*http.Request) error { return nil }, 502, true}} {
		called := false
		mux := http.NewServeMux()
		Register(mux, Config{Authorize: tc.authorize, List: func(*http.Request) ([]domain.Rollout, error) {
			called = true
			return nil, errors.New("private backend diagnostic")
		}})
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/rollouts", nil))
		if response.Code != tc.status || called != tc.read || strings.Contains(response.Body.String(), "private") {
			t.Fatalf("status=%d called=%v %s", response.Code, called, response.Body)
		}
	}
}

func TestAnonymousRolloutWriteIsRefused(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return http.ErrNoCookie }})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/rollouts", strings.NewReader(`{}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestRolloutWriteFailureRedactsBackendDiagnostic(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return nil }, Write: func(*http.Request, Body) (int, any, error) {
		return 0, nil, errors.New("private audit backend diagnostic")
	}})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/rollouts", strings.NewReader(`{}`)))
	if response.Code != 502 || strings.Contains(response.Body.String(), "private") {
		t.Fatal(response.Code, response.Body)
	}
}
