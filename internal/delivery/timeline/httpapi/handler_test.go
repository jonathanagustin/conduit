package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnonymousTimelineIsRefused(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return http.ErrNoCookie }})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/environments/east/timeline", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestTimelineRefusalsPrecedeEvidenceAndRedactDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name      string
		authorize func(*http.Request) error
		read      bool
		status    int
	}{{"missing authorization", nil, false, 401}, {"private identity failure", func(*http.Request) error { return errors.New("private identity") }, false, 401}, {"public refusal", func(*http.Request) error { return StatusError{403, "read permission required"} }, false, 403}, {"backend failure", func(*http.Request) error { return nil }, true, 502}} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			mux := http.NewServeMux()
			Register(mux, Config{Authorize: tc.authorize, Build: func(*http.Request, string) (any, error) {
				called = true
				return nil, errors.New("private backend diagnostic")
			}})
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/environments/east/timeline", nil))
			if response.Code != tc.status || called != tc.read || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("status=%d called=%v body=%s", response.Code, called, response.Body)
			}
		})
	}
}
