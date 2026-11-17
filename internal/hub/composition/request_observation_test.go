package composition

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
	observability "github.com/jonathanagustin/conduit/internal/observability/domain"
)

// lockedBuffer lets a streaming handler's goroutine and the test read one log.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (locked *lockedBuffer) Write(body []byte) (int, error) {
	locked.mutex.Lock()
	defer locked.mutex.Unlock()
	return locked.buffer.Write(body)
}

func (locked *lockedBuffer) String() string {
	locked.mutex.Lock()
	defer locked.mutex.Unlock()
	return locked.buffer.String()
}

type observedHub struct {
	handler  http.Handler
	recorder *metricsapplication.HTTPRecorder
	log      *lockedBuffer
}

// newObservedHub is the production arrangement with the Hub replaced by a
// stub: ObserveRequests outside RateLimit, over the real route register.
func newObservedHub(t *testing.T, inner http.Handler) observedHub {
	t.Helper()
	limited := newLimitedHub(t, productionShaped(nil), inner)
	log := &lockedBuffer{}
	recorder := NewRequestRecorder()
	return observedHub{
		recorder: recorder, log: log,
		handler: ObserveRequests(limited.handler, productionShaped(nil), RequestObservation{
			Recorder: recorder,
			Logger:   slog.New(slog.NewJSONHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug})),
		}),
	}
}

func (hub observedHub) do(method, target string, headers map[string]string, remote string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	if remote != "" {
		request.RemoteAddr = remote
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	hub.handler.ServeHTTP(response, request)
	return response
}

func (hub observedHub) metrics(t *testing.T) string {
	t.Helper()
	gather := metricsapplication.WithHTTPRequests(func(context.Context) (*observability.Registry, error) {
		return observability.NewRegistry(), nil
	}, hub.recorder)
	registry, err := gather(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The publishing path validates, so the test does too: a label the
	// allowlist would refuse must fail here and not in production.
	if err := registry.Validate(); err != nil {
		t.Fatalf("the request metrics would be refused at publish time: %v", err)
	}
	return registry.Render()
}

func (hub observedHub) lines(t *testing.T) []map[string]any {
	t.Helper()
	entries := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(hub.log.String()), "\n") {
		if line == "" {
			continue
		}
		entry := map[string]any{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("the access log is not one JSON object per line: %q", line)
		}
		entries = append(entries, entry)
	}
	return entries
}

func seriesCount(rendered string) int {
	count := 0
	for _, line := range strings.Split(rendered, "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			count++
		}
	}
	return count
}

// The label is the registered pattern. The path carries the tenant's
// environment name; the pattern does not.
func TestARequestIsRecordedUnderItsRegisteredPatternAndNeverItsPath(t *testing.T) {
	t.Parallel()
	hub := newObservedHub(t, nil)
	hub.do(http.MethodGet, "/v1/environments/prod-eu-secret-tenant", operator("alice"), "192.0.2.1:1")
	rendered := hub.metrics(t)
	want := `conduit_http_requests_total{code_class="2xx",method="GET",route="/v1/environments/{environmentID}"} 1`
	if !strings.Contains(rendered, want) {
		t.Fatalf("missing %s in:\n%s", want, rendered)
	}
	if strings.Contains(rendered, "prod-eu-secret-tenant") {
		t.Fatalf("a tenant identifier from the path reached a label:\n%s", rendered)
	}
	if log := hub.log.String(); strings.Contains(log, "prod-eu-secret-tenant") {
		t.Fatalf("a tenant identifier from the path reached the access log:\n%s", log)
	}
	if !strings.Contains(rendered, `conduit_http_request_duration_seconds_count{method="GET",route="/v1/environments/{environmentID}"} 1`) {
		t.Fatalf("the duration histogram did not observe the request:\n%s", rendered)
	}
}

// The property the label allowlist exists for: what a client can type does not
// become series. A thousand identifiers on one route are one route; a thousand
// paths the Hub does not serve are "unmatched".
func TestAThousandDistinctIdentifiersDoNotGrowTheSeriesCount(t *testing.T) {
	t.Parallel()
	// No limiter here: this test is about what becomes a label, and a limiter
	// refusing some of a thousand callers would add status classes of its own.
	hub := observedHub{recorder: NewRequestRecorder(), log: &lockedBuffer{}}
	hub.handler = ObserveRequests(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) }),
		productionShaped(nil), RequestObservation{Recorder: hub.recorder, Logger: slog.New(slog.NewJSONHandler(hub.log, nil))})
	request := func(index int) {
		hub.do(http.MethodGet, fmt.Sprintf("/v1/environments/environment-%d", index), operator(fmt.Sprintf("operator-%d", index)), "")
		hub.do(http.MethodGet, fmt.Sprintf("/not-a-route/%d", index), nil, "")
		hub.do("PROPFIND-"+fmt.Sprint(index), "/v1/environments", nil, "")
	}
	request(0)
	before := seriesCount(hub.metrics(t))
	for index := 1; index < 1000; index++ {
		request(index)
	}
	rendered := hub.metrics(t)
	if after := seriesCount(rendered); after != before {
		t.Fatalf("the series count grew from %d to %d with distinct identifiers", before, after)
	}
	for _, want := range []string{
		`conduit_http_requests_total{code_class="2xx",method="GET",route="/v1/environments/{environmentID}"} 1000`,
		`route="unmatched"`,
		`method="other"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing %s in:\n%s", want, rendered)
		}
	}
	for _, leaked := range []string{"environment-7", "not-a-route", "PROPFIND", "operator-7"} {
		if strings.Contains(rendered, leaked) {
			t.Errorf("%q reached the exposition", leaked)
		}
	}
	// The log is held to the same rule as the labels: a path outside the
	// register is logged as "unmatched", not as what the client typed.
	log := hub.log.String()
	for _, leaked := range []string{"environment-7", "not-a-route", "operator-7"} {
		if strings.Contains(log, leaked) {
			t.Errorf("%q reached the access log", leaked)
		}
	}
	if !strings.Contains(log, `"route":"unmatched"`) {
		t.Errorf("no access-log line names the unmatched route")
	}
}

// One line per request, with the fields an operator searches by and none of
// the ones that carry a credential.
func TestTheAccessLogCarriesNoQueryStringHeaderOrPrincipal(t *testing.T) {
	t.Parallel()
	hub := newObservedHub(t, nil)
	hub.do(http.MethodGet, "/v1/products?access_token=query-secret-9f3a&cursor=tenant-cursor", map[string]string{
		"Authorization": "Bearer header-secret-77c1",
		"Cookie":        "conduit_session=cookie-secret-41bd",
		"X-Api-Key":     "apikey-secret-5e20",
	}, "192.0.2.7:9")
	log := hub.log.String()
	for _, secret := range []string{
		"query-secret-9f3a", "tenant-cursor", "access_token", "header-secret-77c1", "cookie-secret-41bd",
		"apikey-secret-5e20", "192.0.2.7",
	} {
		if strings.Contains(log, secret) {
			t.Errorf("%q reached the access log:\n%s", secret, log)
		}
	}
	lines := hub.lines(t)
	if len(lines) != 1 {
		t.Fatalf("expected one access-log line, got %d:\n%s", len(lines), log)
	}
	line := lines[0]
	for field, want := range map[string]any{
		"msg": "request", "method": "GET", "route": "/v1/products", "status": float64(http.StatusNoContent),
		// The class of caller. "header-secret-77c1" is the operator's name in
		// this fixture, and the loop above already proved it is absent.
		"caller": "operator",
	} {
		if line[field] != want {
			t.Errorf("%s = %v, want %v", field, line[field], want)
		}
	}
	for _, field := range []string{"duration_seconds", "request_id", "trace_id"} {
		if _, present := line[field]; !present {
			t.Errorf("the access log has no %s: %v", field, line)
		}
	}
}

func TestTheCallerClassIsTheLimitersAndIsOneOfThree(t *testing.T) {
	t.Parallel()
	hub := newObservedHub(t, nil)
	hub.do(http.MethodGet, "/v1/products", operator("alice"), "192.0.2.1:1")
	hub.do(http.MethodPost, "/v1/environments/prod/leases", agent("prod", "agent-1"), "192.0.2.2:1")
	hub.do(http.MethodGet, "/v1/products", nil, "192.0.2.3:1")
	hub.do(http.MethodGet, "/health/live", nil, "192.0.2.4:1")
	callers := []string{}
	for _, line := range hub.lines(t) {
		callers = append(callers, fmt.Sprint(line["caller"]))
	}
	if got := strings.Join(callers, ","); got != "operator,agent,anonymous,anonymous" {
		t.Fatalf("caller classes = %s", got)
	}

	// With no limiter in front of the Hub the log classifies the caller itself
	// rather than leaving the field empty.
	log := &lockedBuffer{}
	bare := ObserveRequests(http.NotFoundHandler(), productionShaped(nil), RequestObservation{
		Logger: slog.New(slog.NewJSONHandler(log, nil)),
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
	request.Header.Set("Authorization", "Bearer alice")
	bare.ServeHTTP(httptest.NewRecorder(), request)
	if !strings.Contains(log.String(), `"caller":"operator"`) {
		t.Fatalf("an unlimited Hub did not classify its caller: %s", log.String())
	}
}

// A request id is honoured only when it could not break a header or a log
// line, and one is always returned.
func TestAnInboundRequestIDIsHonouredOnlyWhenWellFormed(t *testing.T) {
	t.Parallel()
	hub := newObservedHub(t, nil)
	accepted := "gateway-7f3a.req_0001"
	if got := hub.do(http.MethodGet, "/health/live", map[string]string{"X-Request-Id": accepted}, "").Header().Get("X-Request-Id"); got != accepted {
		t.Fatalf("a well-formed request id was replaced: %q", got)
	}
	for name, refused := range map[string]string{
		"too long":         strings.Repeat("a", 65),
		"a space":          "abc def",
		"a quote":          `abc"def`,
		"an injected line": "abc\nlevel=ERROR forged",
		"a slash":          "abc/def",
		"non-ASCII":        "abc\u00e9",
	} {
		request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		request.Header["X-Request-Id"] = []string{refused}
		response := httptest.NewRecorder()
		hub.handler.ServeHTTP(response, request)
		got := response.Header().Get("X-Request-Id")
		if got == refused || !observability.ValidRequestID(got) || len(got) != 32 {
			t.Errorf("%s: request id %q was answered with %q", name, refused, got)
		}
	}
	if strings.Contains(hub.log.String(), "forged") {
		t.Fatalf("a refused request id reached the log:\n%s", hub.log.String())
	}
	// Absent: generated, and different each time.
	first := hub.do(http.MethodGet, "/health/live", nil, "").Header().Get("X-Request-Id")
	second := hub.do(http.MethodGet, "/health/live", nil, "").Header().Get("X-Request-Id")
	if first == "" || first == second {
		t.Fatalf("generated request ids %q and %q", first, second)
	}
}

// The caller's trace is continued; a malformed one is not carried.
func TestTheTraceIDIsTheCallersWhenWellFormedAndIsHandedToTheHandler(t *testing.T) {
	t.Parallel()
	var seen observability.TraceContext
	hub := newObservedHub(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen, _ = observability.TraceFromContext(request.Context())
		writer.WriteHeader(http.StatusNoContent)
	}))
	const traceID, parent = "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7"
	hub.do(http.MethodGet, "/v1/products", map[string]string{
		"Authorization": "Bearer alice", "traceparent": "00-" + traceID + "-" + parent + "-01",
	}, "192.0.2.1:1")
	if seen.TraceID != traceID || seen.SpanID == parent || !seen.Valid() || !seen.Sampled {
		t.Fatalf("the handler saw trace %+v", seen)
	}
	if !strings.Contains(hub.log.String(), `"trace_id":"`+traceID+`"`) {
		t.Fatalf("the access log does not carry the caller's trace id:\n%s", hub.log.String())
	}
	hub.do(http.MethodGet, "/v1/products", map[string]string{
		"Authorization": "Bearer alice", "traceparent": "00-not-a-trace-id\" forged=1-00f067aa0ba902b7-01",
	}, "192.0.2.1:1")
	if strings.Contains(hub.log.String(), "forged") || !seen.Valid() || seen.TraceID == traceID {
		t.Fatalf("a malformed traceparent was carried: %+v\n%s", seen, hub.log.String())
	}
}

// A refusal by the limiter is a response the Hub gave, so it is counted.
func TestARequestTheLimiterRefusesIsCountedAndLogged(t *testing.T) {
	t.Parallel()
	hub := newObservedHub(t, nil)
	for range 10 {
		hub.do(http.MethodGet, "/v1/products", nil, "192.0.2.9:1")
	}
	rendered := hub.metrics(t)
	if !strings.Contains(rendered, `conduit_http_requests_total{code_class="4xx",method="GET",route="/v1/products"} 9`) {
		t.Fatalf("refused requests were not counted:\n%s", rendered)
	}
	if !strings.Contains(hub.log.String(), `"status":429`) {
		t.Fatalf("a refused request was not logged:\n%s", hub.log.String())
	}
}

// A stream is counted when it opens, held in its own gauge while it is open,
// and never observed as a duration.
func TestAStreamIsRecordedAtOpenAndCloseAndNotInTheDurationHistogram(t *testing.T) {
	t.Parallel()
	opened, release := make(chan struct{}), make(chan struct{})
	stream := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		// Both ways a handler in this Hub flushes must survive the wrapper.
		writer.(http.Flusher).Flush()
		if err := http.NewResponseController(writer).Flush(); err != nil {
			t.Errorf("the response controller could not reach the connection: %v", err)
		}
		close(opened)
		select {
		case <-release:
		case <-request.Context().Done():
		}
	})
	hub := newObservedHub(t, stream)
	server := httptest.NewServer(hub.handler)
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer alice")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	<-opened

	during := hub.metrics(t)
	for _, want := range []string{
		"conduit_http_streams_open 1",
		"conduit_http_requests_in_flight 0",
		`conduit_http_requests_total{code_class="2xx",method="GET",route="/v1/events"} 1`,
	} {
		if !strings.Contains(during, want) {
			t.Errorf("while the stream is open, missing %q in:\n%s", want, during)
		}
	}
	if !strings.Contains(hub.log.String(), `"msg":"stream opened"`) {
		t.Errorf("the stream's opening was not logged:\n%s", hub.log.String())
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(hub.log.String(), `"msg":"stream closed"`) {
		if time.Now().After(deadline) {
			t.Fatalf("the stream's close was never logged:\n%s", hub.log.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	after := hub.metrics(t)
	if !strings.Contains(after, "conduit_http_streams_open 0") {
		t.Errorf("a closed stream is still counted as open:\n%s", after)
	}
	if strings.Contains(after, "conduit_http_request_duration_seconds") {
		t.Errorf("a stream's lifetime was observed as a request duration:\n%s", after)
	}
	if !strings.Contains(after, `conduit_http_requests_total{code_class="2xx",method="GET",route="/v1/events"} 1`) {
		t.Errorf("the stream was counted twice or not at all:\n%s", after)
	}
}

// Every label the recorder can publish for a route comes from the register.
func TestEveryRouteLabelIsARegisteredPatternWithoutItsMethod(t *testing.T) {
	t.Parallel()
	labels := HubRouteLabels()
	if len(labels) == 0 {
		t.Fatal("the route register produced no labels")
	}
	patterns := map[string]struct{}{}
	for _, route := range HubRouteAuthorization() {
		patterns[route.Pattern] = struct{}{}
	}
	for _, label := range labels {
		if !strings.HasPrefix(label, "/") || strings.ContainsAny(label, " \"\\\n") {
			t.Errorf("route label %q is not a bare path pattern", label)
		}
	}
	if _, served := patterns["GET /v1/events"]; !served {
		t.Fatal("the fixture route these tests stream on is no longer registered")
	}
}

// A handler that panics is recorded as the 500 the client experienced, and the
// panic still reaches net/http.
func TestAPanickingHandlerIsRecordedAsAServerError(t *testing.T) {
	t.Parallel()
	log := &lockedBuffer{}
	recorder := NewRequestRecorder()
	handler := ObserveRequests(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		controlplane.HandlerOptions{}, RequestObservation{Recorder: recorder, Logger: slog.New(slog.NewJSONHandler(log, nil))})
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic was swallowed")
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/products", nil))
	}()
	if !strings.Contains(log.String(), `"status":500`) {
		t.Fatalf("the panic was not logged as a 500: %s", log.String())
	}
}
