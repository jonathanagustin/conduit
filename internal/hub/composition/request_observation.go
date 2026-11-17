package composition

import (
	"bufio"
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/ratelimit"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
	observability "github.com/jonathanagustin/conduit/internal/observability/domain"
)

// RequestObservation is what ObserveRequests records into.
type RequestObservation struct {
	// Recorder holds the request counters. Build it with NewRequestRecorder so
	// it knows the Hub's routes and no others.
	Recorder *metricsapplication.HTTPRecorder
	// Logger receives one access-log line per request. Nil means slog.Default.
	Logger *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// NewRequestRecorder builds the request counters for this Hub's route
// register. The `route` label can take a value from HubRouteLabels or
// "unmatched", and nothing else.
func NewRequestRecorder() *metricsapplication.HTTPRecorder {
	return metricsapplication.NewHTTPRecorder(HubRouteLabels())
}

// HubRouteLabels is every value the `route` label may take, other than
// "unmatched": the path half of each registered pattern.
func HubRouteLabels() []string {
	seen := map[string]struct{}{}
	labels := []string{}
	for _, route := range HubRouteAuthorization() {
		label := routeLabel(route.Pattern)
		if _, duplicate := seen[label]; !duplicate {
			seen[label] = struct{}{}
			labels = append(labels, label)
		}
	}
	return labels
}

// routeLabel is the path half of a ServeMux pattern: "GET /v1/plans/{planID}"
// is "/v1/plans/{planID}". The method is its own label.
func routeLabel(pattern string) string {
	if _, path, found := strings.Cut(pattern, " "); found {
		return path
	}
	return pattern
}

// ObserveRequests is the outermost wrapper of an assembled Hub. For every
// request it assigns a request id and a trace id, counts the request, and
// writes one access-log line. It mounts no route and refuses nothing.
//
// It sits outside RateLimit, on purpose: a request the limiter refuses is a
// request the Hub answered with 429, and an operator looking at the error
// ratio has to see it.
//
// # The route is the registered pattern, never the path
//
// A request for /v1/environments/prod-eu/commands is recorded under
// "/v1/environments/{environmentID}/commands". The path names a tenant's
// environment; the pattern names a route. The pattern is found by asking a
// ServeMux built from HubRouteAuthorization -- the same register the guard and
// the limiter route by -- and a request that register does not match is
// recorded as "unmatched". The path itself is not logged and is not a label.
//
// # What is never logged
//
// The query string, the body, and every header -- so no Authorization, no
// Cookie, no token carried as a parameter. The line carries the method, the
// route pattern, the status, the duration, the class of caller, the request id
// and the trace id. The class of caller is operator, agent or anonymous: the
// kind of principal, not the principal.
//
// # The request id
//
// A caller may supply X-Request-Id, and a gateway usually does. It is honoured
// only when it is at most 64 characters of letters, digits, '.', '_' and '-';
// anything else is replaced by a generated id, never repaired, because the
// value is echoed in a response header and written to the log. The id in use
// is always returned in X-Request-Id, so a person holding a failed response
// can hand an operator the string to search for.
//
// # Streams
//
// A response that turns out to be a server-sent-event stream, or a connection
// a handler takes over, is counted when it opens and held in
// conduit_http_streams_open until it closes. It is not observed in the
// duration histogram: a stream's lifetime is how long somebody kept a console
// tab open, and one of those would move every quantile. Its lifetime is on
// the "stream closed" log line.
func ObserveRequests(hub http.Handler, options controlplane.HandlerOptions, observation RequestObservation) http.Handler {
	logger := observation.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := observation.Now
	if now == nil {
		now = time.Now
	}
	recorder := observation.Recorder
	if recorder == nil {
		recorder = NewRequestRecorder()
	}
	routes := http.NewServeMux()
	for _, route := range HubRouteAuthorization() {
		routes.Handle(route.Pattern, routeDecision(route.Decision))
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := now()
		recorder.Begin()

		requestID := request.Header.Get(observability.RequestIDHeader)
		if !observability.ValidRequestID(requestID) {
			requestID = observability.NewRequestID()
		}
		writer.Header().Set(observability.RequestIDHeader, requestID)

		// The caller's trace when it sent a well-formed one, otherwise a new
		// one. Either way this request is a span of its own within it.
		trace, inherited := observability.ParseTraceparent(request.Header.Get(observability.TraceparentHeader))
		if inherited {
			trace = trace.Child()
		} else {
			trace = observability.NewTraceContext()
		}

		route := UnmatchedRouteLabel
		if matched, pattern := routes.Handler(request); pattern != "" {
			if _, registered := matched.(routeDecision); registered {
				route = routeLabel(pattern)
			}
		}

		caller := &callerClassNote{}
		ctx := observability.ContextWithTrace(request.Context(), trace)
		ctx = context.WithValue(ctx, callerClassKey{}, caller)
		request = request.WithContext(ctx)

		observed := &observedResponse{ResponseWriter: writer}
		line := func(message string, level slog.Level, duration time.Duration) {
			class := caller.class
			if class == "" {
				// No limiter ran in front of the Hub, so nothing classified
				// the caller. Classify it the way the limiter would have.
				class, _, _ = rateLimitSubject(routes, request, options)
				if class == "" {
					class = ratelimit.ClassAnonymous
				}
			}
			logger.LogAttrs(ctx, level, message,
				slog.String("method", request.Method),
				slog.String("route", route),
				slog.Int("status", observed.statusOrDefault()),
				slog.Float64("duration_seconds", duration.Seconds()),
				slog.String("caller", string(class)),
				slog.String("request_id", requestID),
				slog.String("trace_id", trace.TraceID),
			)
		}
		observed.onStream = func() {
			recorder.StreamOpened(route, request.Method, observed.statusOrDefault())
			line("stream opened", slog.LevelInfo, now().Sub(started))
		}

		defer func() {
			duration := now().Sub(started)
			// A handler that panicked wrote nothing a client can use. net/http
			// will close the connection; record what the client experienced
			// and let the panic continue to the server's own recovery.
			failure := recover()
			if failure != nil && !observed.wroteHeader {
				observed.status = http.StatusInternalServerError
			}
			switch {
			case observed.stream:
				recorder.StreamClosed()
				line("stream closed", slog.LevelInfo, duration)
			default:
				recorder.Finish(route, request.Method, observed.statusOrDefault(), duration)
				line("request", accessLogLevel(route, observed.statusOrDefault()), duration)
			}
			if failure != nil {
				panic(failure)
			}
		}()
		hub.ServeHTTP(observed, request)
	})
}

// UnmatchedRouteLabel is the `route` value for a request outside the register.
const UnmatchedRouteLabel = metricsapplication.UnmatchedRoute

// accessLogLevel keeps the kubelet's probes out of an Info-level log without
// hiding them: a probe that succeeded is logged at Debug, and one that failed
// is logged like any other request. They are counted in the metrics either
// way, under their own route.
func accessLogLevel(route string, status int) slog.Level {
	if status < 400 && (route == "/health/live" || route == "/health/ready") {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

type callerClassKey struct{}

// callerClassNote lets RateLimit, which runs inside ObserveRequests and
// resolves who is asking, tell the access log what class it decided on --
// rather than the log resolving the identity a second time and possibly
// disagreeing.
type callerClassNote struct{ class ratelimit.Class }

func noteCallerClass(request *http.Request, class ratelimit.Class) {
	if note, observed := request.Context().Value(callerClassKey{}).(*callerClassNote); observed {
		if class == "" {
			class = ratelimit.ClassAnonymous
		}
		note.class = class
	}
}

// observedResponse records the status a handler wrote and notices a response
// becoming a stream.
//
// It implements Flush and Hijack because handlers in this Hub ask for them by
// type assertion, and Unwrap because the rest use http.ResponseController. A
// wrapper that hid any of the three would break streaming for the routes that
// need it.
type observedResponse struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	stream      bool
	onStream    func()
}

func (response *observedResponse) statusOrDefault() int {
	if response.status == 0 {
		return http.StatusOK
	}
	return response.status
}

func (response *observedResponse) WriteHeader(status int) {
	// 1xx responses other than 101 are informational and may be followed by
	// the real status.
	if !response.wroteHeader && (status >= 200 || status == http.StatusSwitchingProtocols) {
		response.wroteHeader = true
		response.status = status
		if strings.HasPrefix(response.Header().Get("Content-Type"), "text/event-stream") {
			response.becomeStream()
		}
	}
	response.ResponseWriter.WriteHeader(status)
}

func (response *observedResponse) Write(body []byte) (int, error) {
	if !response.wroteHeader {
		response.WriteHeader(http.StatusOK)
	}
	return response.ResponseWriter.Write(body)
}

func (response *observedResponse) Flush() {
	if flusher, supported := response.ResponseWriter.(http.Flusher); supported {
		flusher.Flush()
	}
}

func (response *observedResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, supported := response.ResponseWriter.(http.Hijacker)
	if !supported {
		return nil, nil, http.ErrNotSupported
	}
	connection, buffer, err := hijacker.Hijack()
	if err == nil {
		// The handler now owns the connection and net/http will write no
		// status. Record the takeover as the protocol switch it is.
		if !response.wroteHeader {
			response.wroteHeader = true
			response.status = http.StatusSwitchingProtocols
		}
		response.becomeStream()
	}
	return connection, buffer, err
}

func (response *observedResponse) Unwrap() http.ResponseWriter { return response.ResponseWriter }

func (response *observedResponse) becomeStream() {
	if response.stream {
		return
	}
	response.stream = true
	if response.onStream != nil {
		response.onStream()
	}
}
