package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConsoleHandlerIsMountedOnlyWhenConfigured(t *testing.T) {
	console := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", "console-policy")
		_, _ = w.Write([]byte("console"))
	})
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{ConsoleHandler: console})

	redirect := httptest.NewRecorder()
	handler.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/console", nil))
	if redirect.Code != http.StatusPermanentRedirect || redirect.Header().Get("Location") != "/console/" {
		t.Fatalf("console redirect = %d %q", redirect.Code, redirect.Header().Get("Location"))
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/console/plans/one", nil))
	if response.Code != http.StatusOK || response.Body.String() != "console" {
		t.Fatalf("console response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") != "console-policy" {
		t.Fatalf("console-specific CSP was overwritten: %q", response.Header().Get("Content-Security-Policy"))
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("console route bypassed Hub security headers")
	}

	absent := httptest.NewRecorder()
	NewHandler(NewMemoryRepository()).ServeHTTP(absent, httptest.NewRequest(http.MethodGet, "/console/", nil))
	if absent.Code != http.StatusNotFound {
		t.Fatalf("unconfigured console = %d, want 404", absent.Code)
	}
}
