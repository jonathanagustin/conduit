// Package httpapi serves the committed SSE delivery contract. Native identity,
// permissions, and source polling are supplied by trusted Hub composition.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jonathanagustin/conduit/internal/liveevents/application"
	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

const Route = "GET /v1/events"

type Config struct {
	Service *application.Service
	// Authorize must resolve the current native operator and stream permission.
	// Its context may add trusted request state; the Service rechecks identity.
	Authorize func(*http.Request) (context.Context, error)
	// Refresh drains configured sources outside delivery/repository write locks.
	// A failure terminates the response and is never emitted as an event body.
	Refresh func(context.Context) error
}

type handler struct {
	cfg                                    Config
	heartbeat, lifetime, poll, writeWindow time.Duration
}

func New(cfg Config) (http.Handler, error) {
	if cfg.Service == nil || cfg.Authorize == nil {
		return nil, domain.ErrInvalid
	}
	return &handler{cfg: cfg, heartbeat: domain.HeartbeatInterval, lifetime: domain.ConnectionLifetime, poll: 5 * time.Second, writeWindow: domain.HeartbeatInterval}, nil
}

func (handler *handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	authorized, err := handler.cfg.Authorize(request)
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	if authorized == nil {
		writeError(writer, http.StatusServiceUnavailable, "event delivery unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(authorized, handler.lifetime)
	defer cancel()
	stop := context.AfterFunc(request.Context(), cancel)
	defer stop()
	if !sameOrigin(request) {
		writeError(writer, http.StatusForbidden, "same-origin operator request required")
		return
	}
	if request.URL.RawQuery != "" || request.URL.ForceQuery || (request.Body != nil && request.Body != http.NoBody) || request.ContentLength > 0 || len(request.TransferEncoding) > 0 || len(request.Header.Values("Last-Event-ID")) > 1 {
		writeError(writer, http.StatusBadRequest, "invalid event stream request")
		return
	}
	controller := http.NewResponseController(writer)
	// Refuse wrappers without streaming deadlines before allocating a subscriber.
	// Go's HTTP server supports these; custom middleware must expose Unwrap.
	if err := controller.SetWriteDeadline(time.Now().Add(handler.writeWindow)); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "event streaming unsupported")
		return
	}
	defer controller.SetWriteDeadline(time.Time{})
	if handler.cfg.Refresh != nil && handler.cfg.Refresh(ctx) != nil {
		writeError(writer, http.StatusServiceUnavailable, "event sources unavailable")
		return
	}
	subscription, err := handler.cfg.Service.Open(ctx, request.Header.Get("Last-Event-ID"))
	if err != nil {
		writeDomainError(writer, err)
		return
	}
	defer subscription.Close()
	headers := writer.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-store")
	headers.Set("X-Accel-Buffering", "no")
	headers.Set("X-Conduit-Stream-ID", subscription.Ready.StreamID)
	headers.Set("X-Conduit-Event-Sequence", strconv.FormatUint(subscription.Ready.Sequence, 10))
	headers.Set("X-Conduit-Event-Cursor", subscription.Ready.Cursor)
	writer.WriteHeader(http.StatusOK)
	if err := controller.Flush(); err != nil {
		return
	}
	heartbeat := time.NewTicker(handler.heartbeat)
	defer heartbeat.Stop()
	poll := time.NewTicker(handler.poll)
	defer poll.Stop()
	heartbeatDue := false
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-poll.C:
			if handler.cfg.Refresh != nil && handler.cfg.Refresh(ctx) != nil {
				return
			}
		default:
		}
		envelope, err := subscription.Next(ctx)
		if err != nil {
			return
		}
		if envelope != nil {
			body, err := json.Marshal(envelope)
			if err != nil || len(body) > domain.MaxEnvelopeBytes {
				return
			}
			if err := controller.SetWriteDeadline(time.Now().Add(handler.writeWindow)); err != nil {
				return
			}
			if _, err := fmt.Fprintf(writer, "id: %s\nevent: %s\ndata: %s\n\n", envelope.Cursor, envelope.Kind, body); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
			// A failed write/flush must leave the cursor unacknowledged for replay.
			if err := subscription.Delivered(envelope.Cursor); err != nil {
				return
			}
			continue
		}
		if heartbeatDue {
			heartbeatDue = false
			if err := controller.SetWriteDeadline(time.Now().Add(handler.writeWindow)); err != nil {
				return
			}
			if _, err := writer.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-subscription.Changed():
		case <-poll.C:
			if handler.cfg.Refresh != nil && handler.cfg.Refresh(ctx) != nil {
				return
			}
		case <-heartbeat.C:
			// Re-enter Next before writing: it checks current authorization and must
			// not claim and discard a newly available frame just to send a heartbeat.
			heartbeatDue = true
		}
	}
}

func sameOrigin(request *http.Request) bool {
	if len(request.Header.Values("Origin")) > 1 {
		return false
	}
	origin := request.Header.Get("Origin")
	if origin == "" {
		return request.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	parsed, err := url.Parse(origin)
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return err == nil && parsed.Scheme == scheme && parsed.Host == request.Host && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == ""
}
func writeDomainError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrUnauthorized):
		writeError(writer, http.StatusUnauthorized, "operator identity required")
	case errors.Is(err, domain.ErrForbidden):
		writeError(writer, http.StatusForbidden, "event stream permission required")
	case errors.Is(err, domain.ErrReset):
		writeError(writer, http.StatusConflict, "event stream reset required")
	case errors.Is(err, domain.ErrInvalid):
		writeError(writer, http.StatusBadRequest, "invalid event stream request")
	default:
		writeError(writer, http.StatusServiceUnavailable, "event delivery unavailable")
	}
}
func writeError(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(struct {
		Error string `json:"error"`
	}{message})
}
