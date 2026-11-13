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

// Reader loads one run's progress. A nil reader means this Hub has no
// packaging worker configured; the route still exists so it can refuse
// anonymous callers instead of disappearing.
type Reader interface {
	Read(ctx context.Context, runID string) (domain.RunStatus, error)
}

// Authorize reports whether this caller may read packaging-run progress.
// A nil authorizer refuses everyone.
type Authorize func(*http.Request) error

// Config is the route's dependencies.
type Config struct {
	Reader    Reader
	Authorize Authorize
}

// Register mounts the read-only status route.
func Register(mux *http.ServeMux, cfg Config) {
	mux.HandleFunc(Route, func(writer http.ResponseWriter, request *http.Request) {
		if cfg.Authorize == nil {
			writeError(writer, http.StatusUnauthorized, "unauthorized")
			return
		}
		if err := cfg.Authorize(request); err != nil {
			status := http.StatusUnauthorized
			var coded StatusError
			if errors.As(err, &coded) && coded.Status != 0 {
				status = coded.Status
			}
			message := "unauthorized"
			if err.Error() != "" {
				message = err.Error()
			}
			writeError(writer, status, message)
			return
		}
		status := domain.RunStatus{
			Tier:              "CONDUIT_NATIVE",
			Configured:        cfg.Reader != nil,
			Authority:         "packaging-run",
			PlanAuthority:     "conduit-plan",
			ApprovalAuthority: "conduit-governance",
		}
		if cfg.Reader == nil {
			writeJSON(writer, http.StatusOK, status)
			return
		}
		run, err := cfg.Reader.Read(request.Context(), request.PathValue("runID"))
		if errors.Is(err, ErrNotFound) {
			writeError(writer, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			writeError(writer, http.StatusBadGateway, "packaging run status is unavailable")
			return
		}
		run.Tier = status.Tier
		run.Configured = true
		run.Authority = status.Authority
		run.PlanAuthority = status.PlanAuthority
		run.ApprovalAuthority = status.ApprovalAuthority
		writeJSON(writer, http.StatusOK, run)
	})
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
