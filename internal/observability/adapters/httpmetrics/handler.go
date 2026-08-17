// Package httpmetrics serves the metrics registry over HTTP.
//
// It is a separate listener rather than a route on the Hub's main handler, and
// that is the security decision in this package rather than an accident of
// wiring. The Hub's port carries operator and Agent traffic behind OIDC, mTLS or
// SPIFFE; a scrape endpoint is neither, and bolting an unauthenticated route
// onto an authenticated surface is how one becomes the other. A separate bind
// address lets an operator put it where their scraper is and nowhere else.
//
// It is also off unless configured. An empty address means no listener, so this
// package adds no reachable surface to an existing deployment until somebody
// decides otherwise -- the same shape the insecure-identity opt-ins use.
package httpmetrics

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

// Gather produces the current registry. It takes a context because the Hub's
// numbers come from a database, and a scrape that cannot be cancelled is a
// goroutine that outlives the request that wanted it.
type Gather func(context.Context) (*domain.Registry, error)

// Options configure the handler.
type Options struct {
	// Timeout bounds one gather. A scrape is a diagnostic, so it must fail fast
	// rather than queue behind a struggling database and add load to the
	// incident it is meant to describe.
	Timeout time.Duration
	Logger  *slog.Logger
}

// Handler renders the registry at any path it is mounted on.
//
// Failures are 500 with no body of metrics, never a partial render. A scraper
// that receives half a payload records the half it got, so a truncated scrape
// becomes a set of series that silently stop moving -- which reads as "the
// system is idle" rather than "the exporter broke".
func Handler(gather Gather, options Options) http.Handler {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			http.Error(writer, "metrics are read-only", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), timeout)
		defer cancel()

		registry, err := gather(ctx)
		if err != nil {
			logger.ErrorContext(ctx, "gathering metrics failed", "error", err)
			http.Error(writer, "metrics unavailable", http.StatusInternalServerError)
			return
		}
		// Validated on the serving path, not at construction. A metric added
		// anywhere in the process is checked before it reaches a scraper, which
		// is where a label naming a tenant resource would otherwise escape.
		if err := registry.Validate(); err != nil {
			logger.ErrorContext(ctx, "refusing to publish an invalid metrics registry", "error", err)
			http.Error(writer, "metrics unavailable", http.StatusInternalServerError)
			return
		}

		body := registry.Render()
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		// Nothing here is cacheable: the whole point is the value now.
		writer.Header().Set("Cache-Control", "no-store")
		writer.WriteHeader(http.StatusOK)
		if request.Method == http.MethodHead {
			return
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			logger.ErrorContext(ctx, "writing metrics failed", "error", err)
		}
	})
}
