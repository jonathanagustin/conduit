package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// Every request path this client builds is absolute, so ResolveReference
// discards a base path rather than prefixing it. A Spoke configured with
// CONTROL_PLANE_URL=https://hub.example/platform would send every lease,
// heartbeat and report to https://hub.example/v1/... instead -- and the failure
// presents as the Hub being unreachable rather than as a misconfiguration, so
// the URL is refused rather than silently rewritten.
func TestNewHTTPControlPlaneRefusesAURLWhosePathWouldBeDiscarded(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"https://hub.example/platform",
		"https://hub.example/platform/",
		"https://hub.example/?tenant=a",
		"https://hub.example/#fragment",
	} {
		if _, err := NewHTTPControlPlane(rawURL, nil); err == nil {
			t.Errorf("%s was accepted; its path would be silently discarded", rawURL)
		}
	}
	for _, rawURL := range []string{
		"https://hub.example",
		"https://hub.example/",
		"http://127.0.0.1:8081",
		"  https://hub.example  ",
	} {
		if _, err := NewHTTPControlPlane(rawURL, nil); err != nil {
			t.Errorf("%s was refused: %v", rawURL, err)
		}
	}
	// Non-http schemes and unparseable input stay refused.
	for _, rawURL := range []string{"", "hub.example", "file:///tmp/hub", "ftp://hub.example"} {
		if _, err := NewHTTPControlPlane(rawURL, nil); err == nil {
			t.Errorf("%q was accepted", rawURL)
		}
	}
	// The default client bounds a Hub that accepts a connection and never answers.
	client, err := NewHTTPControlPlane("https://hub.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.Client.Timeout != 30*time.Second {
		t.Fatalf("default timeout = %v, want 30s", client.Client.Timeout)
	}
}

// The Spoke identifies itself by Environment and Agent on every call, and by
// lease token once it holds one. Both are what the Hub authorises against.
func TestAgentRequestsCarryTheirIdentityAndLeaseToken(t *testing.T) {
	t.Parallel()
	var seen http.Header
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, path = r.Header.Clone(), r.URL.Path
		_ = json.NewEncoder(w).Encode(controlplane.PlanRecord{ID: "pln_x"})
	}))
	defer server.Close()
	client, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.Lease(context.Background(), "dev-use2", "agent-dev", time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := seen.Get("X-Platform-Environment-ID"); got != "dev-use2" {
		t.Errorf("environment header = %q", got)
	}
	if got := seen.Get("X-Platform-Agent-ID"); got != "agent-dev" {
		t.Errorf("agent header = %q", got)
	}
	if !strings.Contains(path, "/v1/environments/dev-use2/leases") {
		t.Errorf("lease path = %q", path)
	}

	// A lease-scoped call adds the token; without it the Hub cannot tell this
	// Agent from another holding the same Environment identity.
	if err := client.Start(context.Background(), "dev-use2", "agent-dev", "pln_x", "token-abc"); err != nil {
		t.Fatal(err)
	}
	if got := seen.Get("X-Platform-Lease-Token"); got != "token-abc" {
		t.Errorf("lease token header = %q", got)
	}
}

// The Spoke's own request path carries its own copies of the rules the
// operator adapter also has: what gets a Content-Type, which statuses are
// success, and that 204 short-circuits before any body is read. Pinning them
// on one client says nothing about the other.
func TestHTTPControlPlaneRequestHandling(t *testing.T) {
	t.Parallel()

	newClient := func(t *testing.T, handler http.HandlerFunc) (*HTTPControlPlane, *httptest.Server) {
		t.Helper()
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client, err := NewHTTPControlPlane(server.URL, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		return client, server
	}

	t.Run("a body declares JSON and a bodyless request declares nothing", func(t *testing.T) {
		t.Parallel()
		// A GET that announces a JSON body it does not have is a request some
		// proxies and servers reject outright.
		var withBody, withoutBody, postedBody, gotBody string
		client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			received, _ := io.ReadAll(r.Body)
			if r.Method == http.MethodPost {
				withBody, postedBody = r.Header.Get("Content-Type"), string(received)
			} else {
				withoutBody, gotBody = r.Header.Get("Content-Type"), string(received)
			}
			_, _ = w.Write([]byte(`{}`))
		})
		if _, err := client.request(context.Background(), http.MethodPost, "/v1/x",
			map[string]string{"a": "b"}, nil, &struct{}{}); err != nil {
			t.Fatal(err)
		}
		if _, err := client.request(context.Background(), http.MethodGet, "/v1/x",
			nil, nil, &struct{}{}); err != nil {
			t.Fatal(err)
		}
		if withBody != "application/json" {
			t.Errorf("request with a body declared %q", withBody)
		}
		if withoutBody != "" {
			t.Errorf("request without a body declared %q", withoutBody)
		}
		if postedBody != `{"a":"b"}` {
			t.Errorf("posted body = %q, want the encoded request", postedBody)
		}
		// A nil body must send nothing, not the four bytes json.Marshal
		// produces for nil.
		if gotBody != "" {
			t.Errorf("bodyless request sent %q", gotBody)
		}
	})

	t.Run("204 returns before decoding anything", func(t *testing.T) {
		t.Parallel()
		// The Hub sends 204 with no body. Trying to decode one into the output
		// fails on an empty document, so this has to return first.
		client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		var output struct {
			Name string `json:"name"`
		}
		status, err := client.request(context.Background(), http.MethodDelete, "/v1/x", nil, nil, &output)
		if err != nil {
			t.Fatalf("a 204 was treated as a failure: %v", err)
		}
		if status != http.StatusNoContent {
			t.Errorf("status = %d", status)
		}
	})

	t.Run("the success range is 200 to 299", func(t *testing.T) {
		t.Parallel()
		for status, wantError := range map[int]bool{
			200: false, 201: false, 299: false,
			300: true, 400: true, 500: true,
		} {
			client, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"refused by the Hub"}`))
			})
			_, err := client.request(context.Background(), http.MethodGet, "/v1/x", nil, nil, &struct{}{})
			if (err != nil) != wantError {
				t.Errorf("status %d errored=%v, want %v (%v)", status, err != nil, wantError, err)
				continue
			}
			// A refusal has to quote the Hub, or an operator sees a bare code.
			if wantError && !strings.Contains(err.Error(), "refused by the Hub") {
				t.Errorf("status %d error %q omits the Hub's reason", status, err)
			}
		}
	})

	t.Run("headers the caller supplies are sent", func(t *testing.T) {
		t.Parallel()
		var seen string
		client, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			seen = r.Header.Get("X-Platform-Lease")
			_, _ = w.Write([]byte(`{}`))
		})
		if _, err := client.request(context.Background(), http.MethodGet, "/v1/x", nil,
			map[string]string{"X-Platform-Lease": "token-abc"}, &struct{}{}); err != nil {
			t.Fatal(err)
		}
		if seen != "token-abc" {
			t.Errorf("lease header = %q", seen)
		}
	})
}

// A 410 from the Hub means this Agent's lease is gone -- the plan was
// cancelled or handed to someone else. Renew and Finish both translate it into
// ErrPlanCancelled so the worker stops rather than continuing to apply changes
// on an authority it no longer has. Neither path was reached by any test.
func TestClientTranslatesAGoneLeaseIntoCancellation(t *testing.T) {
	t.Parallel()

	newClient := func(t *testing.T, status int) (*HTTPControlPlane, *[]string) {
		t.Helper()
		paths := &[]string{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*paths = append(*paths, r.URL.Path)
			w.WriteHeader(status)
			if status != http.StatusNoContent {
				_, _ = w.Write([]byte(`{"error":"plan is no longer leased"}`))
			}
		}))
		t.Cleanup(server.Close)
		client, err := NewHTTPControlPlane(server.URL, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		return client, paths
	}

	t.Run("Renew reports cancellation on 410", func(t *testing.T) {
		t.Parallel()
		client, paths := newClient(t, http.StatusGone)
		err := client.Renew(context.Background(), "dev", "agent-dev", "pln_1", "token", time.Minute)
		if !errors.Is(err, ErrPlanCancelled) {
			t.Errorf("error = %v, want ErrPlanCancelled", err)
		}
		// The plan ID is a path segment, not a query, so it must be escaped
		// into one rather than able to introduce another.
		if len(*paths) != 1 || (*paths)[0] != "/v1/plans/pln_1/heartbeat" {
			t.Errorf("paths = %v", *paths)
		}
	})

	t.Run("a plan ID cannot leave its path segment", func(t *testing.T) {
		t.Parallel()
		// The plan ID comes from the Hub, but it is still interpolated into a
		// path. Unescaped, "pln/../cancel" resolves to a different endpoint
		// before the request is sent, and the traversal has been erased by the
		// time anything could notice.
		var seen []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = append(seen, r.URL.EscapedPath())
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		client, err := NewHTTPControlPlane(server.URL, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Renew(context.Background(), "dev", "agent-dev", "pln/../evict", "token", time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := client.Finish(context.Background(), "dev", "agent-dev", "pln/../evict", "token",
			true, "", "", controlplane.PlanExecution{}, controlplane.EnvironmentReport{}); err != nil {
			t.Fatal(err)
		}
		want := []string{
			"/v1/plans/pln%252F..%252Fevict/heartbeat",
			"/v1/plans/pln%252F..%252Fevict/finish",
		}
		if strings.Join(seen, ",") != strings.Join(want, ",") {
			t.Errorf("paths = %v, want the plan ID confined to one segment in both", seen)
		}
	})

	t.Run("Finish reports cancellation on 410", func(t *testing.T) {
		t.Parallel()
		client, paths := newClient(t, http.StatusGone)
		err := client.Finish(context.Background(), "dev", "agent-dev", "pln_1", "token",
			true, "", "", controlplane.PlanExecution{}, controlplane.EnvironmentReport{})
		if !errors.Is(err, ErrPlanCancelled) {
			t.Errorf("error = %v, want ErrPlanCancelled", err)
		}
		if len(*paths) != 1 || (*paths)[0] != "/v1/plans/pln_1/finish" {
			t.Errorf("paths = %v", *paths)
		}
	})

	t.Run("other failures are not cancellation", func(t *testing.T) {
		t.Parallel()
		// A 500 is the Hub having a problem, not the lease being revoked.
		// Treating it as cancellation would abandon a plan that is still ours.
		client, _ := newClient(t, http.StatusInternalServerError)
		err := client.Renew(context.Background(), "dev", "agent-dev", "pln_1", "token", time.Minute)
		if err == nil {
			t.Fatal("a 500 was treated as success")
		}
		if errors.Is(err, ErrPlanCancelled) {
			t.Error("a 500 was treated as a revoked lease")
		}
		if !strings.Contains(err.Error(), "plan is no longer leased") {
			t.Errorf("error %q omits the Hub's response", err)
		}
	})

	t.Run("success is neither", func(t *testing.T) {
		t.Parallel()
		client, _ := newClient(t, http.StatusNoContent)
		if err := client.Renew(context.Background(), "dev", "agent-dev", "pln_1", "token", time.Minute); err != nil {
			t.Errorf("a 204 renewal failed: %v", err)
		}
	})
}

// Health targets and observations are the only endpoints carrying an
// Environment in their path, so they are the ones an Environment name could
// escape from.
func TestClientEscapesTheEnvironmentIntoOnePathSegment(t *testing.T) {
	t.Parallel()
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.EscapedPath())
		_, _ = w.Write([]byte(`{"targets":[]}`))
	}))
	defer server.Close()
	client, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.HealthTargets(context.Background(), "dev/../admin", "agent-dev"); err != nil {
		t.Fatal(err)
	}
	if err := client.ReportHealth(context.Background(), "dev/../admin", "agent-dev", nil); err != nil {
		t.Fatal(err)
	}
	// Asserting the absence of "/../" proves nothing: an unescaped traversal
	// is resolved before the request is sent, so the server sees
	// /v1/environments/admin/... and the symptom has already been erased. The
	// escaped segment has to be asserted positively.
	// The separator ends up doubly encoded: PathEscape turns "/" into "%2F",
	// and ResolveReference then escapes that "%" into "%25". The Hub therefore
	// receives an Environment named "dev%2F..%2Fadmin" rather than
	// "dev/../admin" -- which is harmless here because Environment IDs are DNS
	// labels and cannot legitimately contain either, and the property that
	// matters holds either way: it stays one path segment.
	want := []string{
		"/v1/environments/dev%252F..%252Fadmin/health-targets",
		"/v1/environments/dev%252F..%252Fadmin/health-observations",
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v", seen, want)
	}
}

// A 204 from the lease endpoint means the Hub has no plan for this Agent,
// which is the ordinary idle case rather than a failure. Returning a zero
// PlanRecord and no error instead would have the worker execute an empty plan
// against the Environment on every poll.
func TestClientTranslatesAnEmptyLeaseIntoErrNoPlan(t *testing.T) {
	t.Parallel()

	newClient := func(t *testing.T, status int, body string) *HTTPControlPlane {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			if body != "" {
				_, _ = w.Write([]byte(body))
			}
		}))
		t.Cleanup(server.Close)
		client, err := NewHTTPControlPlane(server.URL, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		return client
	}

	t.Run("204 is no plan, not an empty one", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, http.StatusNoContent, "")
		record, err := client.Lease(context.Background(), "dev", "agent-dev", time.Minute)
		if !errors.Is(err, ErrNoPlan) {
			t.Errorf("error = %v, want ErrNoPlan", err)
		}
		if record.ID != "" {
			t.Errorf("record = %#v, want the zero value", record)
		}
	})

	t.Run("200 carries a plan", func(t *testing.T) {
		t.Parallel()
		client := newClient(t, http.StatusOK, `{"id":"pln_1","status":"LEASED"}`)
		record, err := client.Lease(context.Background(), "dev", "agent-dev", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if record.ID != "pln_1" {
			t.Errorf("record = %#v", record)
		}
	})

	t.Run("a failure is not no-plan", func(t *testing.T) {
		t.Parallel()
		// Treating a Hub error as "nothing to do" makes an unreachable Hub
		// look like an idle Environment, and the Agent polls forever without
		// anyone noticing.
		client := newClient(t, http.StatusInternalServerError, `{"error":"lease store unavailable"}`)
		_, err := client.Lease(context.Background(), "dev", "agent-dev", time.Minute)
		if err == nil {
			t.Fatal("a 500 was treated as success")
		}
		if errors.Is(err, ErrNoPlan) {
			t.Error("a 500 was treated as there being no plan")
		}
		if !strings.Contains(err.Error(), "lease store unavailable") {
			t.Errorf("error %q omits the Hub's reason", err)
		}
	})
}
