package consoleui

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestHandlerServesValidatedBootstrapWithoutSecurityOverrides(t *testing.T) {
	bootstrap := DefaultBootstrap()
	bootstrap.Tenant.ID = "synthetic-alpha"
	bootstrap.Tenant.DisplayName = "Synthetic Alpha"
	bootstrap.Brand.Title = "Alpha Operations"
	bootstrap.Provenance.ClaimClass = "tenant-observation"
	bootstrap.Provenance.EvidenceRef = "evidence://synthetic/tenant-alpha"
	handler := Handler(bootstrap)
	// The bootstrap arrives rendered into the shell, so the assertions are about
	// the document the console actually receives rather than about a second
	// request it no longer makes.
	response := request(t, handler, "/console/")
	if response.Code != http.StatusOK {
		t.Fatalf("shell = %d %q", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), bootstrapMarker) {
		t.Fatal("the shell was served with its bootstrap marker unrendered")
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("shell CSP = %q", response.Header().Get("Content-Security-Policy"))
	}
	served := servedBootstrap(t, response.Body.String())
	if served != bootstrap {
		t.Fatalf("served bootstrap = %#v, want %#v", served, bootstrap)
	}
	// The shape stays closed across the change of transport: a field the type
	// does not declare cannot reach the console, whatever the tenant file says.
	//
	// Scoped to the injected document rather than the whole page, which now
	// carries the shell's own <script> tag. Checking the page would pass for the
	// wrong reason on some of these names and fail for the wrong reason on
	// others.
	injected := servedBootstrapJSON(t, response.Body.String())
	for _, forbidden := range []string{"capabilities", "apiBaseUrl", "script", "authMode", "csp"} {
		if strings.Contains(injected, forbidden) {
			t.Fatalf("injected bootstrap contains forbidden field %q", forbidden)
		}
	}
	// The route the console used to fetch is gone rather than left serving a
	// second, drifting copy of the same document.
	if gone := request(t, handler, "/console/bootstrap.json"); gone.Code != http.StatusNotFound {
		t.Fatalf("the retired bootstrap route answered %d", gone.Code)
	}
}

// servedBootstrap recovers the injected document the way a browser does:
// unescape the attribute, then parse. Doing it by hand rather than with a
// regexp over the raw HTML is what makes the escaping itself part of the test.
func servedBootstrapJSON(t *testing.T, document string) string {
	t.Helper()
	opening := `<meta name="` + BootstrapMetaName + `" content="`
	start := strings.Index(document, opening)
	if start < 0 {
		t.Fatalf("the shell carries no %s meta", BootstrapMetaName)
	}
	rest := document[start+len(opening):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatal("the bootstrap meta is unterminated")
	}
	return html.UnescapeString(rest[:end])
}

func servedBootstrap(t *testing.T, document string) Bootstrap {
	t.Helper()
	var served Bootstrap
	if err := json.Unmarshal([]byte(servedBootstrapJSON(t, document)), &served); err != nil {
		t.Fatalf("the injected bootstrap did not parse: %v", err)
	}
	return served
}

// A tenant name may legally contain a double quote -- the validator bans "<"
// and ">" and not the quote -- so an unescaped injection would close the
// attribute early and hand the console a truncated document. This is the bug
// class the previous fetch-a-JSON-document design could not have, so it is
// asserted rather than assumed.
func TestATenantNameContainingAQuoteSurvivesInjection(t *testing.T) {
	bootstrap := DefaultBootstrap()
	bootstrap.Tenant.DisplayName = `Alpha "Ops`
	bootstrap.Brand.Title = `Alpha "Ops`
	response := request(t, Handler(bootstrap), "/console/")
	if response.Code != http.StatusOK {
		t.Fatalf("shell = %d", response.Code)
	}
	if served := servedBootstrap(t, response.Body.String()); served != bootstrap {
		t.Fatalf("a quoted tenant name did not survive injection: %#v", served)
	}
}

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

// A build that dropped the marker would produce a shell the console refuses,
// and the operator would meet "console configuration is unavailable" with a
// perfectly healthy Hub. The Handler refuses to construct instead, so the
// failure appears where its cause is.
//
// Asserted against a shell that has no marker rather than by deleting the real
// one, because the real one is what verify-console-assets.sh pins.
func TestHydrationRefusesAShellWithNoMarker(t *testing.T) {
	if _, err := hydrateShellDocument([]byte(`<html><head></head></html>`), []byte(`{}`)); err == nil {
		t.Fatal("a shell with no bootstrap marker was accepted")
	}
	hydrated, err := hydrateShellDocument([]byte("<head>"+bootstrapMarker+"</head>"), []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("a shell carrying the marker was refused: %v", err)
	}
	if strings.Contains(string(hydrated), bootstrapMarker) {
		t.Fatal("the marker survived hydration")
	}
	if !strings.Contains(string(hydrated), BootstrapMetaName) {
		t.Fatalf("hydration produced no bootstrap meta: %s", hydrated)
	}
}
