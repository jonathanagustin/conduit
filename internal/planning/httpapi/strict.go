package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
	api "github.com/jonathanagustin/conduit/internal/planning/transport"
)

type requestContext struct {
	service Sessions
	stream  bool
}
type requestKey struct{}

// Register mounts the generated router and strict server. Authentication and
// owner-store selection precede body parsing and every application call.
func Register(mux *http.ServeMux, cfg Config) {
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Authorize == nil || cfg.Authorize(r) != nil {
				writeError(w, 401, "", "unauthorized")
				return
			}
			service := cfg.Service
			if cfg.ResolveService != nil {
				var err error
				service, err = cfg.ResolveService(r)
				if err != nil {
					service = nil
				}
			}
			if service == nil {
				writeError(w, 401, "", "unauthorized")
				return
			}
			w.Header().Set("x-content-type-options", "nosniff")
			if !checkRequestBody(w, r) {
				return
			}
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/turns") {
				_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(TurnWriteWindow))
			}
			ctx := context.WithValue(r.Context(), requestKey{}, requestContext{service: service, stream: strings.Contains(r.Header.Get("Accept"), "text/event-stream")})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	strict := api.NewStrictHandlerWithOptions(strictServer{}, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, _ error) {
			writeError(w, 400, "", "request body must be a JSON object")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			var failure wireError
			if errors.As(err, &failure) {
				writeError(w, failure.status, failure.code, failure.message)
				return
			}
			writeDomainError(w, err)
		},
	})
	api.HandlerWithOptions(strict, api.StdHTTPServerOptions{BaseRouter: mux, Middlewares: []api.MiddlewareFunc{guard}})
}

// Preserve strict unknown-field and size rejection around the generated body
// decoder. Both validators decode into the same generated request DTOs.
func checkRequestBody(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		return true
	}
	var destination any
	optional := false
	switch {
	case r.URL.Path == "/v1/planning-sessions":
		destination = &api.CreatePlanningSessionJSONRequestBody{}
	case strings.HasSuffix(r.URL.Path, "/answers"):
		destination = &api.AnswerPlanningNeedsInputJSONRequestBody{}
	case strings.HasSuffix(r.URL.Path, "/turns"):
		destination = &api.PostPlanningTurnJSONRequestBody{}
		optional = true
	default:
		return true
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "", "request body is larger than the limit")
		} else {
			writeError(w, http.StatusBadRequest, "", "request body could not be read")
		}
		return false
	}
	if optional && len(bytes.TrimSpace(data)) == 0 {
		r.Body = io.NopCloser(bytes.NewReader(data))
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err == nil {
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			err = errors.New("trailing JSON")
		}
	}
	if err != nil || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		writeError(w, 400, "", "request body must be a JSON object")
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return true
}

type wireError struct {
	status        int
	code, message string
}

func (err wireError) Error() string { return err.message }

type strictServer struct{}

var _ api.StrictServerInterface = strictServer{}

// The DTO translation is deliberately at the transport edge. The application
// continues to own native types and never imports generated wire models.
func dto[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}
func service(ctx context.Context) Sessions { return ctx.Value(requestKey{}).(requestContext).service }

func (strictServer) CreatePlanningSession(ctx context.Context, r api.CreatePlanningSessionRequestObject) (api.CreatePlanningSessionResponseObject, error) {
	if r.Body == nil {
		return nil, wireError{400, "", "request body must be a JSON object"}
	}
	session, err := service(ctx).Create(ctx, r.Body.RepoUrl, r.Body.Ref)
	if err != nil {
		return nil, err
	}
	return dto[api.CreatePlanningSession201JSONResponse](session)
}
func (strictServer) GetPlanningSession(ctx context.Context, r api.GetPlanningSessionRequestObject) (api.GetPlanningSessionResponseObject, error) {
	session, err := service(ctx).Get(ctx, r.SessionID)
	if err != nil {
		return nil, err
	}
	return dto[api.GetPlanningSession200JSONResponse](session)
}
func (strictServer) AcceptPlanningSession(ctx context.Context, r api.AcceptPlanningSessionRequestObject) (api.AcceptPlanningSessionResponseObject, error) {
	session, err := service(ctx).Accept(ctx, r.SessionID)
	if err != nil {
		return nil, err
	}
	return dto[api.AcceptPlanningSession200JSONResponse](session)
}
func (strictServer) AnswerPlanningNeedsInput(ctx context.Context, r api.AnswerPlanningNeedsInputRequestObject) (api.AnswerPlanningNeedsInputResponseObject, error) {
	if r.Body == nil {
		return nil, wireError{400, "", "request body must be a JSON object"}
	}
	answers := make([]domain.Answer, len(r.Body.Answers))
	for i, a := range r.Body.Answers {
		answers[i] = domain.Answer{ID: a.Id, Value: a.Value}
	}
	session, err := service(ctx).Answer(ctx, r.SessionID, answers)
	if err != nil {
		return nil, err
	}
	return dto[api.AnswerPlanningNeedsInput200JSONResponse](session)
}
func (strictServer) GetPlanningBinding(ctx context.Context, r api.GetPlanningBindingRequestObject) (api.GetPlanningBindingResponseObject, error) {
	session, err := service(ctx).Get(ctx, r.SessionID)
	if err != nil {
		return nil, err
	}
	if session.Binding == nil {
		return nil, wireError{409, domain.CodeNotAccepted, "the planning session has not been accepted"}
	}
	return dto[api.GetPlanningBinding200JSONResponse](session.Binding)
}
func (strictServer) StartPlanningCompute(ctx context.Context, r api.StartPlanningComputeRequestObject) (api.StartPlanningComputeResponseObject, error) {
	session, err := service(ctx).Compute(ctx, r.SessionID)
	if err != nil {
		return nil, err
	}
	return dto[api.StartPlanningCompute202JSONResponse](session)
}
func (strictServer) ListPlanningEvents(ctx context.Context, r api.ListPlanningEventsRequestObject) (api.ListPlanningEventsResponseObject, error) {
	events, err := service(ctx).Events(ctx, r.SessionID)
	if err != nil {
		return nil, err
	}
	if events == nil {
		events = []domain.Event{}
	}
	items, err := dto[[]api.Event](events)
	if err != nil {
		return nil, err
	}
	return api.ListPlanningEvents200JSONResponse{Items: items}, nil
}
func (strictServer) PostPlanningTurn(ctx context.Context, r api.PostPlanningTurnRequestObject) (api.PostPlanningTurnResponseObject, error) {
	message := ""
	if r.Body != nil && r.Body.Message != nil {
		message = *r.Body.Message
	}
	prior := 0
	if existing, err := service(ctx).Get(ctx, r.SessionID); err == nil {
		prior = len(existing.Events)
	}
	session, err := service(ctx).Turn(ctx, r.SessionID, message)
	if err != nil {
		return nil, err
	}
	if !ctx.Value(requestKey{}).(requestContext).stream {
		return dto[api.PostPlanningTurn200JSONResponse](session)
	}
	if prior > len(session.Events) {
		prior = 0
	}
	var buffer bytes.Buffer
	for _, event := range session.Events[prior:] {
		entry, err := dto[api.Event](event)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buffer, "id: %s\nevent: %s\ndata: %s\n\n", entry.Id, entry.Type, data)
	}
	return turnStream{PostPlanningTurn200TexteventStreamResponse: api.PostPlanningTurn200TexteventStreamResponse{Body: bytes.NewReader(buffer.Bytes())}}, nil
}

type turnStream struct {
	api.PostPlanningTurn200TexteventStreamResponse
}

func (response turnStream) VisitPostPlanningTurnResponse(w http.ResponseWriter) error {
	w.Header().Set("cache-control", "no-cache")
	return response.PostPlanningTurn200TexteventStreamResponse.VisitPostPlanningTurnResponse(w)
}
