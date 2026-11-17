package composition

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/ratelimit"
)

type limiterClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *limiterClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *limiterClock) Advance(by time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(by)
}

// bearerOperators is a production-shaped resolver: a request is an operator
// only when it presents a credential, and otherwise is nobody.
func bearerOperators(resolved *atomic.Int64) controlplane.OperatorIdentityResolver {
	return func(request *http.Request) (controlplane.IdentityClaims, error) {
		if resolved != nil {
			resolved.Add(1)
		}
		name, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !found || name == "" {
			return controlplane.IdentityClaims{}, controlplane.ErrUnauthorized
		}
		return controlplane.IdentityClaims{Username: name}, nil
	}
}

type limitedHub struct {
	handler http.Handler
	clock   *limiterClock
	limiter *ratelimit.Limiter
	served  *atomic.Int64
}

// newLimitedHub wraps a stub that counts what reaches it. The routes are the
// real register's; only what is behind them is replaced.
func newLimitedHub(t *testing.T, options controlplane.HandlerOptions, inner http.Handler) limitedHub {
	t.Helper()
	clock := &limiterClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	limiter, err := ratelimit.New(ratelimit.Config{
		Operator:    ratelimit.Limit{PerSecond: 1, Burst: 3},
		Agent:       ratelimit.Limit{PerSecond: 1, Burst: 2},
		Anonymous:   ratelimit.Limit{PerSecond: 0.5, Burst: 1},
		MaximumKeys: 100, IdleTimeout: time.Minute, Now: clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	served := &atomic.Int64{}
	if inner == nil {
		inner = http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	}
	counted := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		served.Add(1)
		inner.ServeHTTP(writer, request)
	})
	return limitedHub{handler: RateLimit(counted, options, limiter), clock: clock, limiter: limiter, served: served}
}

type limitedRequest struct {
	method, path, remote string
	headers              map[string]string
}

func (hub limitedHub) do(request limitedRequest) *httptest.ResponseRecorder {
	if request.method == "" {
		request.method = http.MethodGet
	}
	built := httptest.NewRequest(request.method, request.path, nil)
	if request.remote != "" {
		built.RemoteAddr = request.remote
	}
	for name, value := range request.headers {
		built.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	hub.handler.ServeHTTP(response, built)
	return response
}

func operator(name string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + name}
}

func agent(environment, id string) map[string]string {
	return map[string]string{"X-Platform-Environment-ID": environment, "X-Platform-Agent-ID": id}
}

func productionShaped(resolved *atomic.Int64) controlplane.HandlerOptions {
	return controlplane.HandlerOptions{
		OperatorIdentityResolver: bearerOperators(resolved), AllowInsecureAgentIdentityHeaders: true,
	}
}

func TestAnOperatorPastItsBurstIsRefusedWith429AndRetryAfter(t *testing.T) {
	t.Parallel()
	hub := newLimitedHub(t, productionShaped(nil), nil)
	for index := range 3 {
		if response := hub.do(limitedRequest{path: "/v1/products", headers: operator("alice")}); response.Code != http.StatusNoContent {
			t.Fatalf("request %d inside the burst = %d", index, response.Code)
		}
	}
	refused := hub.do(limitedRequest{path: "/v1/products", headers: operator("alice")})
	if refused.Code != http.StatusTooManyRequests {
		t.Fatalf("request past the burst = %d, want 429", refused.Code)
	}
	// One token a second: Retry-After is whole seconds, and never zero.
	if retry := refused.Header().Get("Retry-After"); retry != "1" {
		t.Fatalf("Retry-After = %q, want 1", retry)
	}
	var body map[string]string
	if err := json.Unmarshal(refused.Body.Bytes(), &body); err != nil || body["status"] != "429" || body["error"] == "" ||
		!strings.HasPrefix(refused.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("429 body = %q (%v), want the Hub's JSON error shape", refused.Body.String(), err)
	}
	if hub.served.Load() != 3 {
		t.Fatalf("%d requests reached the Hub; the refused one must not", hub.served.Load())
	}
	// Honouring Retry-After is enough to be served again.
	hub.clock.Advance(time.Second)
	if response := hub.do(limitedRequest{path: "/v1/products", headers: operator("alice")}); response.Code != http.StatusNoContent {
		t.Fatalf("after waiting Retry-After = %d", response.Code)
	}
}

func TestOneThrottledOperatorDoesNotThrottleAnother(t *testing.T) {
	t.Parallel()
	hub := newLimitedHub(t, productionShaped(nil), nil)
	for range 10 {
		hub.do(limitedRequest{path: "/v1/plans", headers: operator("alice"), remote: "192.0.2.10:4000"})
	}
	// Same remote address on purpose: operators behind one gateway or NAT
	// are told apart by who they are, not where they connect from.
	for index := range 3 {
		response := hub.do(limitedRequest{path: "/v1/plans", headers: operator("bob"), remote: "192.0.2.10:4000"})
		if response.Code != http.StatusNoContent {
			t.Fatalf("bob's request %d = %d while alice was throttled", index, response.Code)
		}
	}
}

// The reason the limiter has classes. Operator and anonymous traffic is driven
// far past its limits from the Agent's own address; the Agent's lease, health
// and report routes must still answer.
func TestAgentRoutesAreServedWhileOperatorsAndAnonymousCallersAreThrottled(t *testing.T) {
	t.Parallel()
	hub := newLimitedHub(t, productionShaped(nil), nil)
	const remote = "192.0.2.10:4000"
	for range 50 {
		hub.do(limitedRequest{path: "/v1/products", headers: operator("alice"), remote: remote})
		hub.do(limitedRequest{path: "/v1/products", remote: remote})
	}
	if response := hub.do(limitedRequest{path: "/v1/products", headers: operator("alice"), remote: remote}); response.Code != http.StatusTooManyRequests {
		t.Fatalf("the operator was not throttled: %d", response.Code)
	}
	for _, route := range []limitedRequest{
		{method: http.MethodPost, path: "/v1/environments/prod/leases"},
		{method: http.MethodPost, path: "/v1/plans/plan-1/heartbeat"},
	} {
		route.headers, route.remote = agent("prod", "agent-1"), remote
		if response := hub.do(route); response.Code != http.StatusNoContent {
			t.Fatalf("%s %s = %d while operators were throttled", route.method, route.path, response.Code)
		}
	}
	// The Agent class is limited too, per Agent.
	throttled := hub.do(limitedRequest{method: http.MethodPost, path: "/v1/plans/plan-1/finish", headers: agent("prod", "agent-1"), remote: remote})
	other := hub.do(limitedRequest{method: http.MethodPost, path: "/v1/plans/plan-1/finish", headers: agent("prod", "agent-2"), remote: remote})
	if throttled.Code != http.StatusTooManyRequests || other.Code != http.StatusNoContent {
		t.Fatalf("agent-1 past its burst = %d, agent-2 = %d", throttled.Code, other.Code)
	}
	counts := hub.limiter.Snapshot()
	if counts[ratelimit.ClassAgent].Allowed != 3 || counts[ratelimit.ClassAgent].Limited != 1 ||
		counts[ratelimit.ClassOperator].Limited == 0 || counts[ratelimit.ClassAnonymous].Limited == 0 {
		t.Fatalf("snapshot = %+v", counts)
	}
}

// An Agent credential buys the Agent budget only on an Agent route, and an
// Agent route without one is an anonymous request like any other.
func TestTheAgentClassIsOnlyForAgentIdentitiesOnAgentRoutes(t *testing.T) {
	t.Parallel()
	hub := newLimitedHub(t, productionShaped(nil), nil)
	lease := limitedRequest{method: http.MethodPost, path: "/v1/environments/prod/leases", remote: "198.51.100.1:1"}
	if first, second := hub.do(lease), hub.do(lease); first.Code != http.StatusNoContent || second.Code != http.StatusTooManyRequests {
		t.Fatalf("unidentified lease requests = %d then %d, want the anonymous burst of 1", first.Code, second.Code)
	}
	// Agent headers on an operator route resolve no operator: anonymous.
	asAgent := limitedRequest{path: "/v1/products", headers: agent("prod", "agent-1"), remote: "198.51.100.2:1"}
	if first, second := hub.do(asAgent), hub.do(asAgent); first.Code != http.StatusNoContent || second.Code != http.StatusTooManyRequests {
		t.Fatalf("agent identity on an operator route = %d then %d, want the anonymous burst of 1", first.Code, second.Code)
	}
	if counts := hub.limiter.Snapshot(); counts[ratelimit.ClassAgent].Allowed != 0 || counts[ratelimit.ClassOperator].Allowed != 0 {
		t.Fatalf("an unidentified caller spent an authenticated budget: %+v", counts)
	}
}

func TestAnonymousCallersAreKeyedByThePeerAddressAndNeverByForwardedHeaders(t *testing.T) {
	t.Parallel()
	hub := newLimitedHub(t, productionShaped(nil), nil)
	first := hub.do(limitedRequest{path: "/v1/products", remote: "203.0.113.5:1000"})
	if first.Code != http.StatusNoContent {
		t.Fatalf("first anonymous request = %d", first.Code)
	}
	// A different source port is the same peer, and a forwarding header the
	// client wrote itself is not a different one.
	for index := range 5 {
		response := hub.do(limitedRequest{path: "/v1/products", remote: fmt.Sprintf("203.0.113.5:%d", 2000+index), headers: map[string]string{
			"X-Forwarded-For": fmt.Sprintf("10.0.0.%d", index), "Forwarded": fmt.Sprintf("for=10.1.0.%d", index),
			"X-Real-IP": fmt.Sprintf("10.2.0.%d", index),
		}})
		if response.Code != http.StatusTooManyRequests {
			t.Fatalf("a spoofed forwarding header bought a fresh bucket: %d", response.Code)
		}
	}
	if response := hub.do(limitedRequest{path: "/v1/products", remote: "203.0.113.6:1000"}); response.Code != http.StatusNoContent {
		t.Fatalf("a different peer shared the throttled peer's bucket: %d", response.Code)
	}
	// 0.5 tokens a second: the wait is two seconds.
	if retry := hub.do(limitedRequest{path: "/v1/products", remote: "203.0.113.5:1"}).Header().Get("Retry-After"); retry != "2" {
		t.Fatalf("anonymous Retry-After = %q, want 2", retry)
	}
}

func TestRemoteAddressKeyGroupsAnIPv6SubscriberAndUnmapsIPv4(t *testing.T) {
	t.Parallel()
	for remote, want := range map[string]string{
		"203.0.113.5:443":              "203.0.113.5",
		"[::ffff:203.0.113.5]:443":     "203.0.113.5",
		"[2001:db8:1:2:aaaa::1]:443":   "2001:db8:1:2::/64",
		"[2001:db8:1:2:bbbb::9]:8443":  "2001:db8:1:2::/64",
		"[2001:db8:1:3::1]:443":        "2001:db8:1:3::/64",
		"[fe80::1%eth0]:443":           "fe80::/64",
		"203.0.113.5":                  "203.0.113.5",
		"":                             "unknown",
		"@":                            "unknown",
		"not an address, x-forwarded:": "unknown",
	} {
		if got := remoteAddressKey(remote); got != want {
			t.Errorf("remoteAddressKey(%q) = %q, want %q", remote, got, want)
		}
	}
}

// A probe refused for load would restart a Hub for being busy.
func TestHealthProbesAreNeverLimited(t *testing.T) {
	t.Parallel()
	hub := newLimitedHub(t, productionShaped(nil), nil)
	for _, path := range []string{"/health/live", "/health/ready"} {
		for index := range 25 {
			if response := hub.do(limitedRequest{path: path, remote: "192.0.2.1:1"}); response.Code != http.StatusNoContent {
				t.Fatalf("%s probe %d = %d", path, index, response.Code)
			}
		}
	}
	if counts := hub.limiter.Snapshot(); counts[ratelimit.ClassAnonymous].Allowed != 0 {
		t.Fatalf("probes spent tokens: %+v", counts)
	}
}

// A path the Hub does not serve is limited like any anonymous request, and no
// credential is verified to decide that.
func TestUnregisteredAndPublicRoutesSpendTheAnonymousBudgetWithoutResolvingAnybody(t *testing.T) {
	t.Parallel()
	var resolved atomic.Int64
	hub := newLimitedHub(t, productionShaped(&resolved), nil)
	for _, path := range []string{"/v1/probe/secrets", "/version"} {
		request := limitedRequest{path: path, headers: operator("alice"), remote: "192.0.2.77:1"}
		if path == "/version" {
			request.remote = "192.0.2.78:1"
		}
		if first, second := hub.do(request), hub.do(request); first.Code != http.StatusNoContent || second.Code != http.StatusTooManyRequests {
			t.Fatalf("%s = %d then %d, want the anonymous burst of 1", path, first.Code, second.Code)
		}
	}
	if resolved.Load() != 0 {
		t.Fatalf("the operator resolver ran %d times for routes that consult no identity", resolved.Load())
	}
}

// A stream is admitted once, when it opens. With a burst of one, the open
// stream must deliver everything it writes; only the next open is refused.
func TestALongLivedStreamIsCountedAtOpenAndNotThrottledWhileOpen(t *testing.T) {
	t.Parallel()
	const frames = 200
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	stream := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		for index := range frames {
			_, _ = fmt.Fprintf(writer, "data: %d\n\n", index)
			writer.(http.Flusher).Flush()
			if index == frames/2 {
				select {
				case <-release:
				case <-request.Context().Done():
					return
				}
			}
		}
	})
	hub := newLimitedHub(t, productionShaped(nil), stream)
	server := httptest.NewServer(hub.handler)
	defer server.Close()

	open := func() *http.Response {
		response, err := http.Get(server.URL + "/v1/events")
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	first := open()
	defer first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("opening the stream = %d", first.StatusCode)
	}
	// While the first stream is mid-flight, the budget is spent: a second
	// open is refused.
	second := open()
	_ = second.Body.Close()
	if second.StatusCode != http.StatusTooManyRequests || second.Header.Get("Retry-After") == "" {
		t.Fatalf("second open = %d with Retry-After %q, want 429", second.StatusCode, second.Header.Get("Retry-After"))
	}
	releaseOnce.Do(func() { close(release) })
	payload := make([]byte, 0, 4096)
	buffer := make([]byte, 4096)
	for {
		count, err := first.Body.Read(buffer)
		payload = append(payload, buffer[:count]...)
		if err != nil {
			break
		}
	}
	if got := strings.Count(string(payload), "data: "); got != frames || !strings.Contains(string(payload), "data: "+strconv.Itoa(frames-1)) {
		t.Fatalf("the open stream delivered %d of %d frames", got, frames)
	}
	if counts := hub.limiter.Snapshot()[ratelimit.ClassAnonymous]; counts.Allowed != 1 || counts.Limited != 1 {
		t.Fatalf("a stream of %d frames was counted as %+v, want one admission", frames, counts)
	}
}

// End to end against the assembled Hub, not a stub: a throttled operator gets
// 429 from a real index route, an Agent still reaches a real lease handler,
// and a nil limiter leaves the Hub as it was.
func TestTheAssembledHubIsLimitedByClass(t *testing.T) {
	options := controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, AllowInsecureAgentIdentityHeaders: true}
	assembled := NewHubHandler(controlplane.NewMemoryRepository(), options)
	limiter, err := ratelimit.New(ratelimit.Config{
		Operator: ratelimit.Limit{PerSecond: 0.001, Burst: 2}, Agent: ratelimit.Limit{PerSecond: 0.001, Burst: 2},
		Anonymous: ratelimit.Limit{PerSecond: 0.001, Burst: 1}, MaximumKeys: 10, IdleTimeout: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	limited := RateLimit(assembled, options, limiter)
	get := func(handler http.Handler, method, path string, headers map[string]string) int {
		request := httptest.NewRequest(method, path, strings.NewReader("{}"))
		for name, value := range headers {
			request.Header.Set(name, value)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	alice := map[string]string{"X-Platform-Subject": "alice"}
	for index := range 2 {
		if code := get(limited, http.MethodGet, "/v1/products", alice); code != http.StatusOK {
			t.Fatalf("operator read %d = %d", index, code)
		}
	}
	if code := get(limited, http.MethodGet, "/v1/products", alice); code != http.StatusTooManyRequests {
		t.Fatalf("operator read past the burst = %d, want 429", code)
	}
	if code := get(limited, http.MethodPost, "/v1/environments/prod/leases", agent("prod", "agent-1")); code == http.StatusTooManyRequests {
		t.Fatal("the Agent's lease request was throttled by operator traffic")
	}
	if code := get(RateLimit(assembled, options, nil), http.MethodGet, "/v1/products", alice); code != http.StatusOK {
		t.Fatalf("a nil limiter changed the Hub's answer: %d", code)
	}
}
