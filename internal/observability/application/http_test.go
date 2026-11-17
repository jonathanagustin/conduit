package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/application"
	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

func empty(context.Context) (*domain.Registry, error) { return domain.NewRegistry(), nil }

func renderHTTP(t *testing.T, recorder *application.HTTPRecorder) string {
	t.Helper()
	registry, err := application.WithHTTPRequests(empty, recorder)(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate(); err != nil {
		t.Fatalf("the request metrics would be refused at publish time: %v", err)
	}
	return registry.Render()
}

func TestRequestsAreCountedByRouteMethodAndStatusClass(t *testing.T) {
	t.Parallel()
	recorder := application.NewHTTPRecorder([]string{"/v1/plans/{planID}"})
	for _, status := range []int{200, 204, 301, 404, 429, 500, 503} {
		recorder.Begin()
		recorder.Finish("/v1/plans/{planID}", "GET", status, 30*time.Millisecond)
	}
	recorder.Begin()
	rendered := renderHTTP(t, recorder)
	for _, want := range []string{
		`conduit_http_requests_total{code_class="2xx",method="GET",route="/v1/plans/{planID}"} 2`,
		`conduit_http_requests_total{code_class="3xx",method="GET",route="/v1/plans/{planID}"} 1`,
		`conduit_http_requests_total{code_class="4xx",method="GET",route="/v1/plans/{planID}"} 2`,
		`conduit_http_requests_total{code_class="5xx",method="GET",route="/v1/plans/{planID}"} 2`,
		"# TYPE conduit_http_requests_total counter",
		"# TYPE conduit_http_request_duration_seconds histogram",
		`conduit_http_request_duration_seconds_bucket{le="0.025",method="GET",route="/v1/plans/{planID}"} 0`,
		`conduit_http_request_duration_seconds_bucket{le="0.05",method="GET",route="/v1/plans/{planID}"} 7`,
		`conduit_http_request_duration_seconds_bucket{le="+Inf",method="GET",route="/v1/plans/{planID}"} 7`,
		`conduit_http_request_duration_seconds_count{method="GET",route="/v1/plans/{planID}"} 7`,
		"conduit_http_requests_in_flight 1",
		"conduit_http_streams_open 0",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing %q in:\n%s", want, rendered)
		}
	}
}

// The recorder bounds its own labels. A caller that passed the raw path -- the
// mistake this type is built to survive -- publishes "unmatched", not the path.
func TestTheRecorderRefusesToMintALabelFromWhatItIsHanded(t *testing.T) {
	t.Parallel()
	recorder := application.NewHTTPRecorder([]string{"/v1/plans/{planID}"})
	for index := range 1000 {
		recorder.Begin()
		recorder.Finish(fmt.Sprintf("/v1/plans/plan-%d", index), fmt.Sprintf("METHOD%d", index), 200, time.Millisecond)
	}
	rendered := renderHTTP(t, recorder)
	if !strings.Contains(rendered, `conduit_http_requests_total{code_class="2xx",method="other",route="unmatched"} 1000`) {
		t.Fatalf("unknown routes and methods were not folded into the catch-all:\n%s", rendered)
	}
	if strings.Contains(rendered, "plan-") || strings.Contains(rendered, "METHOD") {
		t.Fatalf("a caller-supplied value became a label:\n%s", rendered)
	}
	if got := strings.Count(rendered, "conduit_http_requests_total{"); got != 1 {
		t.Fatalf("1000 distinct paths produced %d request series", got)
	}
}

func TestADurationAboveTheLastBoundLandsOnlyInInf(t *testing.T) {
	t.Parallel()
	recorder := application.NewHTTPRecorder([]string{"/slow"})
	recorder.Begin()
	recorder.Finish("/slow", "POST", 200, 45*time.Second)
	rendered := renderHTTP(t, recorder)
	if !strings.Contains(rendered, `conduit_http_request_duration_seconds_bucket{le="30",method="POST",route="/slow"} 0`) ||
		!strings.Contains(rendered, `conduit_http_request_duration_seconds_bucket{le="+Inf",method="POST",route="/slow"} 1`) ||
		!strings.Contains(rendered, `conduit_http_request_duration_seconds_sum{method="POST",route="/slow"} 45`) {
		t.Fatalf("a slow request was bucketed wrongly:\n%s", rendered)
	}
}

func TestAStreamMovesFromInFlightToOpenAndIsNotObservedAsADuration(t *testing.T) {
	t.Parallel()
	recorder := application.NewHTTPRecorder([]string{"/v1/events"})
	recorder.Begin()
	recorder.StreamOpened("/v1/events", "GET", 200)
	open := renderHTTP(t, recorder)
	if !strings.Contains(open, "conduit_http_streams_open 1") || !strings.Contains(open, "conduit_http_requests_in_flight 0") ||
		!strings.Contains(open, `conduit_http_requests_total{code_class="2xx",method="GET",route="/v1/events"} 1`) {
		t.Fatalf("an open stream is not legible:\n%s", open)
	}
	recorder.StreamClosed()
	closed := renderHTTP(t, recorder)
	if !strings.Contains(closed, "conduit_http_streams_open 0") || strings.Contains(closed, "conduit_http_request_duration_seconds") {
		t.Fatalf("a closed stream left the wrong trace:\n%s", closed)
	}
}

func TestANilRecorderAddsNothingAndAFailedGatherStaysFailed(t *testing.T) {
	t.Parallel()
	registry, err := application.WithHTTPRequests(empty, nil)(t.Context())
	if err != nil || registry.Render() != "" {
		t.Fatalf("a nil recorder changed the registry: %q %v", registry.Render(), err)
	}
	failing := func(context.Context) (*domain.Registry, error) { return nil, context.DeadlineExceeded }
	if registry, err := application.WithHTTPRequests(failing, application.NewHTTPRecorder(nil))(t.Context()); err == nil || registry != nil {
		t.Fatal("a failed gather was turned into a registry")
	}
}
