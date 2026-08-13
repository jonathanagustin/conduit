package consoleui

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestHandlerServesShellRouteAndImmutableAsset(t *testing.T) {
	handler := Handler()
	shell := request(t, handler, "/console/")
	if shell.Code != http.StatusOK || !strings.Contains(shell.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("shell = %d %q", shell.Code, shell.Body.String())
	}
	if got := shell.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("shell cache control = %q", got)
	}
	if !strings.Contains(shell.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("console CSP = %q", shell.Header().Get("Content-Security-Policy"))
	}

	route := request(t, handler, "/console/plans/pln_example")
	if route.Code != http.StatusOK || route.Body.String() != shell.Body.String() {
		t.Fatalf("SPA route = %d, same shell = %t", route.Code, route.Body.String() == shell.Body.String())
	}

	match := regexp.MustCompile(`/console/assets/[^"']+\.js`).FindString(shell.Body.String())
	if match == "" {
		t.Fatalf("shell names no hashed JavaScript asset: %q", shell.Body.String())
	}
	asset := request(t, handler, match)
	if asset.Code != http.StatusOK || asset.Body.Len() == 0 {
		t.Fatalf("asset = %d, bytes = %d", asset.Code, asset.Body.Len())
	}
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache control = %q", got)
	}
	if !strings.HasPrefix(asset.Header().Get("ETag"), `"sha256-`) {
		t.Fatalf("asset ETag = %q", asset.Header().Get("ETag"))
	}
}

func TestHandlerFailsClosedForMissingAssetsAndNonCanonicalPaths(t *testing.T) {
	handler := Handler()
	for _, target := range []string{
		"/console/assets/missing.js",
		"/console/../v1/plans",
		"/not-console/",
	} {
		response := request(t, handler, target)
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, response.Code)
		}
	}
}

func TestHandlerHonorsConditionalAssetRead(t *testing.T) {
	handler := Handler()
	shell := request(t, handler, "/console/")
	match := regexp.MustCompile(`/console/assets/[^"']+\.js`).FindString(shell.Body.String())
	asset := request(t, handler, match)

	request := httptest.NewRequest(http.MethodGet, match, nil)
	request.Header.Set("If-None-Match", asset.Header().Get("ETag"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotModified {
		t.Fatalf("conditional GET = %d, want 304", response.Code)
	}
}

func request(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
