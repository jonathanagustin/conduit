// Package httpapi serves packaging-run progress. It does not accept an
// approval and it does not report Plan status. Plan status stays on the
// existing Plan route; the plan id in this payload is only a reference.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

const Route = "GET /v1/packaging-runs/{runID}"

// ErrNotFound is returned by a reader when the run id is unknown.
var ErrNotFound = errors.New("packaging run not found")

// StatusError is an authorization or request failure with an HTTP status.
type StatusError struct {
	Status  int
	Message string
}

func (e StatusError) Error() string { return e.Message }

// Reader loads packaging-run progress. A nil reader means this Hub has no
// packaging worker configured; the routes still exist so they can refuse
// anonymous callers instead of disappearing. List and Events are reads.
// Nothing on this interface approves, cancels, or submits a Plan.
type Reader interface {
	Read(ctx context.Context, runID string) (domain.RunStatus, error)
	List(ctx context.Context) ([]domain.RunStatus, error)
	Events(ctx context.Context, runID string) ([]domain.StepEvent, error)
}

// Authorize reports whether this caller may read packaging-run progress.
// A nil authorizer refuses everyone.
type Authorize func(*http.Request) error

// Config is the route's dependencies. Visible is the per-row check for the
// index and the history. Permit is the governance decision for cancel and
// rerun. The single-run route stays any authenticated operator.
type Config struct {
	Reader    Reader
	Authorize Authorize
	Visible   Visible
	Permit    Permit
	Controls  Controls
	Decisions DecisionLog
}

// Register mounts the packaging-run routes. Cancel and rerun do not approve
// and do not cancel a Plan.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(Route, func(writer http.ResponseWriter, request *http.Request) {
		if !authorized(cfg, writer, request) {
			return
		}
		status, err := readStatus(request.Context(), cfg, request.PathValue("runID"))
		if errors.Is(err, ErrNotFound) {
			writeError(writer, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			writeError(writer, http.StatusBadGateway, "packaging run status is unavailable")
			return
		}
		writeJSON(writer, http.StatusOK, status)
	})
	mountCollections(mux, cfg)
	mountControls(mux, cfg)
}

// authorize applies the route's caller check. A nil authorizer refuses
// everyone, which is what an unconfigured deployment must do.
func authorize(cfg Config, request *http.Request) error {
	if cfg.Authorize == nil {
		return StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
	}
	return cfg.Authorize(request)
}

func refusalStatus(err error) (int, string) {
	status := http.StatusUnauthorized
	var coded StatusError
	if errors.As(err, &coded) && coded.Status != 0 {
		status = coded.Status
	}
	message := "unauthorized"
	if err.Error() != "" {
		message = err.Error()
	}
	return status, message
}

// readStatus loads one run the same way the REST route does: a nil reader is
// an unconfigured worker, and a found run is stamped and redacted.
func readStatus(ctx context.Context, cfg Config, runID string) (domain.RunStatus, error) {
	if cfg.Reader == nil {
		return stamp(domain.RunStatus{}, false), nil
	}
	run, err := cfg.Reader.Read(ctx, runID)
	if err != nil {
		return domain.RunStatus{}, err
	}
	return redactRun(run), nil
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"error": message, "status": itoa(status)})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [8]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[i:])
}
