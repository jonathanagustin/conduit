package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnonymousUpgradeReadIsRefused(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return http.ErrNoCookie }})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/fleet/upgrades", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestUpgradeReadRefusalsDoNotDiscloseBackendErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		authorize func(*http.Request) error
		backend   bool
		status    int
	}{
		{"missing authorizer", nil, false, http.StatusUnauthorized},
		{"authentication failure", func(*http.Request) error { return errors.New("private credential diagnostic") }, false, http.StatusUnauthorized},
		{"public refusal", func(*http.Request) error {
			return StatusError{Status: http.StatusForbidden, Message: "read permission required"}
		}, false, http.StatusForbidden},
		{"backend failure", func(*http.Request) error { return nil }, true, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/v1/fleet/upgrades", "/v1/products/example/dependencies/1.0.0"} {
				called := false
				read := func() (any, error) { called = true; return nil, errors.New("private backend diagnostic") }
				mux := http.NewServeMux()
				Register(mux, Config{Authorize: tc.authorize, Upgrades: func(*http.Request) (any, error) { return read() }, Dependencies: func(*http.Request, string, string) (any, error) { return read() }})
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != tc.status || called != tc.backend || strings.Contains(response.Body.String(), "private") {
					t.Fatalf("%s: status=%d backend=%v body=%s", path, response.Code, called, response.Body)
				}
			}
		})
	}
}
