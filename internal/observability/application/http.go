package application

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

// UnmatchedRoute is the one label value every request outside the route
// register shares: a path the Hub does not serve, a scanner's guesses, a typo.
// One bucket, so that what a client can type never becomes a series.
const UnmatchedRoute = "unmatched"

// otherMethod is the bucket for a method outside the closed set. HTTP lets a
// client send any token as a method, so the method is not bounded by itself.
const otherMethod = "other"

// httpMethods is the closed set of values the `method` label may take.
var httpMethods = map[string]struct{}{
	"GET": {}, "HEAD": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {}, "OPTIONS": {},
}

// httpDurationBounds are the request-duration buckets, in seconds.
//
// The top is 30 s because the Hub's own write timeout is of that order: a
// request slower than the last finite bound has almost certainly been cut off,
// and +Inf says so. The bottom is 5 ms because a health probe and a cached
// read both sit there and an operator wants to see them as fast, not as "under
// 100 ms".
var httpDurationBounds = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

type requestSeries struct{ route, method, codeClass string }

type durationSeries struct{ route, method string }

type durationHistogram struct {
	buckets []uint64
	sum     float64
}

// HTTPRecorder is the Hub's request counters, held in the process.
//
// # What bounds it
//
// Every label value is drawn from a closed set that this type owns: the routes
// it was constructed with plus "unmatched", seven methods plus "other", and
// five status classes. The caller passes whatever it has; Record maps anything
// outside those sets into the catch-all rather than trusting the caller to
// have done it. The consequence is that the series count is at most
// routes x methods x classes, and that a caller which mistakenly passed the
// raw request path -- which carries tenant identifiers -- would publish
// "unmatched" and not the path.
//
// # Counters live in the process, by design
//
// The Hub runs more than one replica and each counts what it served. That is
// what a Prometheus counter is: the scraper reads each replica and sums. The
// queue gauges come from the database instead because a queue is one shared
// thing; a request is not.
type HTTPRecorder struct {
	mutex       sync.Mutex
	routes      map[string]struct{}
	requests    map[requestSeries]uint64
	durations   map[durationSeries]*durationHistogram
	inFlight    int64
	streamsOpen int64
}

// NewHTTPRecorder builds a recorder that will name these routes and no others.
func NewHTTPRecorder(routes []string) *HTTPRecorder {
	known := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		known[route] = struct{}{}
	}
	return &HTTPRecorder{
		routes: known, requests: map[requestSeries]uint64{}, durations: map[durationSeries]*durationHistogram{},
	}
}

// Begin counts a request as in flight.
func (recorder *HTTPRecorder) Begin() {
	recorder.mutex.Lock()
	recorder.inFlight++
	recorder.mutex.Unlock()
}

// Finish records a request that has been answered in full: one count, one
// duration observation, and one fewer in flight.
func (recorder *HTTPRecorder) Finish(route, method string, status int, duration time.Duration) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.inFlight--
	route, method = recorder.bounded(route, method)
	recorder.requests[requestSeries{route, method, codeClass(status)}]++
	series := durationSeries{route, method}
	histogram := recorder.durations[series]
	if histogram == nil {
		histogram = &durationHistogram{buckets: make([]uint64, len(httpDurationBounds)+1)}
		recorder.durations[series] = histogram
	}
	seconds := duration.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	histogram.sum += seconds
	histogram.buckets[sort.SearchFloat64s(httpDurationBounds, seconds)]++
}

// StreamOpened records a long-lived response at the moment its headers are
// written, and moves it from "in flight" to "stream open".
//
// A live-event stream stays open for as long as a console tab does. Counting
// it when it ends would hide it for exactly the period it is consuming a
// connection, and observing its lifetime in the duration histogram would put
// every stream in +Inf and make the p99 of an idle Hub thirty minutes. So a
// stream is counted when it opens, held in its own gauge while it is open, and
// never observed as a duration. Its lifetime is on the access-log line written
// when it closes.
func (recorder *HTTPRecorder) StreamOpened(route, method string, status int) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	recorder.inFlight--
	recorder.streamsOpen++
	route, method = recorder.bounded(route, method)
	recorder.requests[requestSeries{route, method, codeClass(status)}]++
}

// StreamClosed records the end of a stream StreamOpened recorded.
func (recorder *HTTPRecorder) StreamClosed() {
	recorder.mutex.Lock()
	recorder.streamsOpen--
	recorder.mutex.Unlock()
}

func (recorder *HTTPRecorder) bounded(route, method string) (string, string) {
	if _, known := recorder.routes[route]; !known {
		route = UnmatchedRoute
	}
	if _, known := httpMethods[method]; !known {
		method = otherMethod
	}
	return route, method
}

// codeClass maps a status to one of five values. A status outside 100-599
// cannot be written by net/http, but a handler that never wrote one is
// recorded by the caller as 200, which is what net/http sends.
func codeClass(status int) string {
	switch {
	case status >= 500:
		return "5xx"
	case status >= 400:
		return "4xx"
	case status >= 300:
		return "3xx"
	case status >= 200:
		return "2xx"
	default:
		return "1xx"
	}
}

// WithHTTPRequests adds the recorder's counts to a gathered registry.
func WithHTTPRequests(
	gather func(context.Context) (*domain.Registry, error), recorder *HTTPRecorder,
) func(context.Context) (*domain.Registry, error) {
	if recorder == nil {
		return gather
	}
	return func(ctx context.Context) (*domain.Registry, error) {
		registry, err := gather(ctx)
		if err != nil {
			return nil, err
		}
		recorder.mutex.Lock()
		defer recorder.mutex.Unlock()
		for series, count := range recorder.requests {
			registry.Add(domain.Metric{
				Name: "conduit_http_requests_total", Kind: domain.KindCounter, Value: float64(count),
				Help: "requests the Hub answered, by registered route pattern, method and status class; " +
					"a stream is counted when it opens",
				Labels: map[string]string{"route": series.route, "method": series.method, "code_class": series.codeClass},
			})
		}
		for series, histogram := range recorder.durations {
			registry.AddHistogram(domain.Histogram{
				Name:   "conduit_http_request_duration_seconds",
				Help:   "time from a request arriving to its response being complete; streams are not observed",
				Labels: map[string]string{"route": series.route, "method": series.method},
				Bounds: httpDurationBounds, Buckets: append([]uint64(nil), histogram.buckets...), Sum: histogram.sum,
			})
		}
		registry.Add(domain.Metric{
			Name: "conduit_http_requests_in_flight", Kind: domain.KindGauge, Value: float64(recorder.inFlight),
			Help: "requests this replica has received and not yet answered, excluding open streams",
		})
		registry.Add(domain.Metric{
			Name: "conduit_http_streams_open", Kind: domain.KindGauge, Value: float64(recorder.streamsOpen),
			Help: "long-lived responses (server-sent events, upgraded connections) this replica holds open",
		})
		return registry, nil
	}
}
