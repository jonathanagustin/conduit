package httpmetrics_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/adapters/httpmetrics"
	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func serve(t *testing.T, gather httpmetrics.Gather, method string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/metrics", nil)
	httpmetrics.Handler(gather, httpmetrics.Options{Logger: quiet()}).ServeHTTP(recorder, request)
	return recorder
}

func healthy(context.Context) (*domain.Registry, error) {
	return domain.NewRegistry(domain.Metric{
		Name: "conduit_reconciliation_work_pending", Kind: domain.KindGauge,
		Help: "reconciliation work due and not leased", Value: 3,
	}), nil
}

func TestAScrapeRendersTheRegistry(t *testing.T) {
	t.Parallel()
	recorder := serve(t, healthy, http.MethodGet)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("content type is %q; a scraper reads text", got)
	}
	body := recorder.Body.String()
	for _, required := range []string{
		"# HELP conduit_reconciliation_work_pending",
		"# TYPE conduit_reconciliation_work_pending gauge",
		"conduit_reconciliation_work_pending 3",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("body is missing %q:\n%s", required, body)
		}
	}
}

// A truncated or partial scrape is worse than a failed one. A scraper that
// receives half a payload records the half it got, so the missing series stop
// moving rather than going absent -- which reads as "idle" instead of "broken".
func TestAGatherFailureServesNoMetricsAtAll(t *testing.T) {
	t.Parallel()
	recorder := serve(t, func(context.Context) (*domain.Registry, error) {
		return nil, errors.New("the database is unreachable")
	}, http.MethodGet)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "conduit_") {
		t.Errorf("a failed gather still emitted metrics:\n%s", recorder.Body.String())
	}
}

// The validation that keeps tenant identifiers out of labels has to run on the
// path that publishes, not at construction. A metric assembled anywhere in the
// process reaches a scraper through here, and here is the last place to stop it.
func TestAnInvalidRegistryIsRefusedRatherThanPublished(t *testing.T) {
	t.Parallel()
	recorder := serve(t, func(context.Context) (*domain.Registry, error) {
		return domain.NewRegistry(domain.Metric{
			Name: "conduit_work_pending", Kind: domain.KindGauge, Help: "h", Value: 1,
			Labels: map[string]string{"environment": "customer-prod"},
		}), nil
	}, http.MethodGet)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "customer-prod") {
		t.Errorf("the tenant identifier reached the response body:\n%s", recorder.Body.String())
	}
}

// Read-only by construction. Metrics carry no state to change, so anything but
// a read is a caller that has misunderstood the endpoint, and answering it
// invites the misunderstanding to grow.
func TestOnlyReadsAreAnswered(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		recorder := serve(t, healthy, method)
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s returned %d, want 405", method, recorder.Code)
		}
		if allow := recorder.Header().Get("Allow"); allow == "" {
			t.Errorf("%s refusal does not say what is allowed", method)
		}
	}
}

// HEAD answers the status and the headers without the body, which is what a
// liveness style check wants and what a scraper occasionally sends first.
func TestHeadAnswersWithoutABody(t *testing.T) {
	t.Parallel()
	recorder := serve(t, healthy, http.MethodHead)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("HEAD returned a body of %d bytes", recorder.Body.Len())
	}
}

// A scrape is a diagnostic. If it queues behind a struggling database it adds
// load to the incident it exists to describe, so it is bounded and fails.
func TestAGatherThatHangsIsBoundedByTheTimeout(t *testing.T) {
	t.Parallel()
	released := make(chan struct{})
	defer close(released)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	handler := httpmetrics.Handler(func(ctx context.Context) (*domain.Registry, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-released:
			return healthy(ctx)
		}
	}, httpmetrics.Options{Timeout: 20 * time.Millisecond, Logger: quiet()})

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(recorder, request)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a hanging gather was not bounded by the timeout")
	}
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500 after the gather timed out", recorder.Code)
	}
}
