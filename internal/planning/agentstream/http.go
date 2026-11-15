package agentstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// Route accepts the canonical run input. Snapshot recovery is a new POST/run,
// never Last-Event-ID re-entry into a broken exchange.
const Route = "POST /v1/agent-runs"

// HTTPConfig selects an owner-scoped service after authenticating the request.
type HTTPConfig struct {
	Authorize      func(*http.Request) error
	ResolveService func(*http.Request) (*Service, error)
}

// RegisterHTTP mounts the run adapter without changing other planning routes.
func RegisterHTTP(mux *http.ServeMux, config HTTPConfig) {
	mux.HandleFunc(Route, func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if config.Authorize == nil || config.Authorize(request) != nil || config.ResolveService == nil {
			httpRunError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "Operator authentication is required.")
			return
		}
		service, err := config.ResolveService(request)
		if err != nil || service == nil {
			httpRunError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "Operator authentication is required.")
			return
		}
		mediaType, _, mediaErr := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if mediaErr != nil || mediaType != "application/json" {
			httpRunError(writer, http.StatusUnsupportedMediaType, "INVALID_CONTENT_TYPE", "Use application/json.")
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, 1<<20))
		if err != nil {
			httpRunError(writer, http.StatusRequestEntityTooLarge, "INPUT_TOO_LARGE", "The run input could not be read within its size bound.")
			return
		}
		input, err := DecodeRunInput(raw)
		if err != nil {
			httpRunError(writer, http.StatusBadRequest, "INVALID_RUN_INPUT", ErrInvalidInput.Error())
			return
		}
		runContext, cancel := context.WithTimeout(request.Context(), 30*time.Minute)
		defer cancel()
		started := false
		var writeMu sync.Mutex
		control := http.NewResponseController(writer)
		_ = control.SetWriteDeadline(time.Now().Add(30 * time.Minute))
		stopped := make(chan struct{})
		var heartbeat sync.WaitGroup
		heartbeat.Add(1)
		go func() {
			defer heartbeat.Done()
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopped:
					return
				case <-runContext.Done():
					return
				case <-ticker.C:
					writeMu.Lock()
					if started {
						if _, err := writer.Write([]byte(": keep-alive\n\n")); err != nil {
							cancel()
						}
						if err := control.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
							cancel()
						}
					}
					writeMu.Unlock()
				}
			}
		}()
		defer func() { close(stopped); heartbeat.Wait() }()
		err = service.Run(runContext, input, func(raw []byte) error {
			writeMu.Lock()
			defer writeMu.Unlock()
			if !started {
				writer.Header().Set("Content-Type", "text/event-stream")
				writer.Header().Set("X-Accel-Buffering", "no")
				writer.WriteHeader(http.StatusOK)
				started = true
			}
			if _, err := writer.Write(append(append([]byte("data: "), raw...), []byte("\n\n")...)); err != nil {
				return err
			}
			if err := control.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
				return err
			}
			return nil
		})
		if err == nil || started {
			return
		}
		code, status, message := "RUN_UNAVAILABLE", http.StatusBadGateway, "The planning run is unavailable."
		switch {
		case errors.Is(err, domain.ErrNotFound):
			code, status, message = "SESSION_NOT_FOUND", http.StatusNotFound, "The planning session was not found."
		case errors.Is(err, ErrInvalidResume):
			code, status, message = "INVALID_RESUME", http.StatusConflict, ErrInvalidResume.Error()
		case errors.Is(err, ErrRunBusy), errors.Is(err, ErrRunExists):
			code, status, message = "RUN_CONFLICT", http.StatusConflict, "Use a new run ID after the active run finishes."
		case errors.Is(err, ErrRunCapacity):
			code, status, message = "RUN_CAPACITY", http.StatusTooManyRequests, ErrRunCapacity.Error()
		case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidToolSchema):
			code, status, message = "INVALID_RUN_INPUT", http.StatusBadRequest, ErrInvalidInput.Error()
		case errors.Is(err, ErrAuditUnavailable):
			code, status, message = "AUDIT_UNAVAILABLE", http.StatusServiceUnavailable, ErrAuditUnavailable.Error()
		}
		httpRunError(writer, status, code, message)
	})
}

func httpRunError(writer http.ResponseWriter, status int, code, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"code": code, "error": message})
}
