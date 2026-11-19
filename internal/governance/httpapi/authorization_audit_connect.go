package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	governancev1 "github.com/jonathanagustin/conduit/gen/go/conduit/governance/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/governance/v1/governancev1connect"
	"github.com/jonathanagustin/conduit/internal/governance/application"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const AuthorizationAuditChainConnectRoute = "POST /conduit.governance.v1.AuthorizationAuditChainReadService/ListAuthorizationAuditChain"

const AuthorizationAuditConnectRoute = "POST /conduit.governance.v1.AuthorizationAuditReadService/ListAuthorizationAuditEvents"

// AuthorizationAuditConfig binds an original authenticated administrator carrier.
type AuthorizationAuditConfig struct {
	Authorize func(*http.Request) (context.Context, error)
	Read      func(context.Context, application.AuditEventReadQuery) (application.AuditEventReadPage, error)
	ReadChain func(context.Context, application.AuditChainReadQuery) (application.AuditChainReadPage, error)
}

// RegisterAuthorizationAuditConnect authorizes before decoding the request.
func RegisterAuthorizationAuditConnect(mux *http.ServeMux, cfg AuthorizationAuditConfig) {
	server := connect.NewServer()
	governancev1connect.RegisterAuthorizationAuditReadServiceHandler(server, &auditReadService{cfg: cfg})
	governancev1connect.RegisterAuthorizationAuditChainReadServiceHandler(server, &auditReadService{cfg: cfg})
	rpc := http.NewServeMux()
	connecthttp.Mount(rpc, server)
	authorized := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ctx context.Context
		err := application.ErrAuditReadUnauthenticated
		if cfg.Authorize != nil {
			ctx, err = cfg.Authorize(r)
		}
		if err != nil || ctx == nil {
			if err == nil {
				err = application.ErrAuditReadUnauthenticated
			}
			w.Header().Set("X-Content-Type-Options", "nosniff")
			_ = connecthttp.NewErrorWriter().Write(w, r, auditReadError(err))
			return
		}
		rpc.ServeHTTP(w, r.WithContext(ctx))
	})
	mux.Handle(AuthorizationAuditConnectRoute, authorized)
	mux.Handle(AuthorizationAuditChainConnectRoute, authorized)
}

type auditReadService struct{ cfg AuthorizationAuditConfig }

func auditReadError(err error) error {
	code := connect.CodeUnavailable
	switch {
	case errors.Is(err, application.ErrInvalidAuditRead):
		code = connect.CodeInvalidArgument
	case errors.Is(err, application.ErrAuditReadUnauthenticated):
		code = connect.CodeUnauthenticated
	case errors.Is(err, application.ErrAuditReadForbidden):
		code = connect.CodePermissionDenied
	}
	return connect.NewError(code, code.String())
}

func (s *auditReadService) ListAuthorizationAuditEvents(ctx context.Context, r *governancev1.ListAuthorizationAuditEventsRequest) (*governancev1.ListAuthorizationAuditEventsResponse, error) {
	q := application.AuditEventReadQuery{Cursor: r.Cursor, ResourceRID: r.ResourceRid, Descending: r.Descending}
	if r.Limit != nil {
		n := int(*r.Limit)
		q.Limit = &n
	}
	if r.Since != nil {
		if r.Since.CheckValid() != nil {
			return nil, auditReadError(application.ErrInvalidAuditRead)
		}
		t := r.Since.AsTime()
		q.Since = &t
	}
	if r.Until != nil {
		if r.Until.CheckValid() != nil {
			return nil, auditReadError(application.ErrInvalidAuditRead)
		}
		t := r.Until.AsTime()
		q.Until = &t
	}
	if s.cfg.Read == nil {
		return nil, auditReadError(errors.New("unavailable"))
	}
	page, err := s.cfg.Read(ctx, q)
	if err != nil {
		return nil, auditReadError(err)
	}
	out := &governancev1.ListAuthorizationAuditEventsResponse{Total: int64(page.Total), PageSize: int32(page.PageSize), Limit: int32(page.Limit), NextCursor: page.NextCursor, HasPrevious: page.HasPrevious}
	for _, e := range page.Events {
		event := &governancev1.AuthorizationAuditEvent{Id: e.ID, SchemaVersion: e.SchemaVersion, Categories: e.Categories, TraceId: e.TraceID, Subject: e.Subject, Action: e.Action, ResourceRid: e.ResourceRID, Decision: e.Decision, Reason: e.Reason}
		event.RequestFieldsJson, err = json.Marshal(e.RequestFields)
		if err != nil {
			return nil, auditReadError(err)
		}
		event.ResultFieldsJson, err = json.Marshal(e.ResultFields)
		if err != nil {
			return nil, auditReadError(err)
		}
		if len(e.Context) > 0 {
			event.ContextJson, err = json.Marshal(e.Context)
			if err != nil {
				return nil, auditReadError(err)
			}
		}
		if !e.CreatedAt.IsZero() {
			event.CreatedAt = timestamppb.New(e.CreatedAt)
			if err = event.CreatedAt.CheckValid(); err != nil {
				return nil, auditReadError(err)
			}
		}
		out.Events = append(out.Events, event)
	}
	return out, nil
}

func (s *auditReadService) ListAuthorizationAuditChain(ctx context.Context, r *governancev1.ListAuthorizationAuditChainRequest) (*governancev1.ListAuthorizationAuditChainResponse, error) {
	q := application.AuditChainReadQuery{Cursor: r.Cursor, ResourceRID: r.ResourceRid, Descending: r.Descending}
	if r.Limit != nil {
		n := int(*r.Limit)
		q.Limit = &n
	}
	if r.Since != nil {
		if r.Since.CheckValid() != nil {
			return nil, auditReadError(application.ErrInvalidAuditRead)
		}
		t := r.Since.AsTime()
		q.Since = &t
	}
	if r.Until != nil {
		if r.Until.CheckValid() != nil {
			return nil, auditReadError(application.ErrInvalidAuditRead)
		}
		t := r.Until.AsTime()
		q.Until = &t
	}
	if s.cfg.ReadChain == nil {
		return nil, auditReadError(errors.New("unavailable"))
	}
	page, err := s.cfg.ReadChain(ctx, q)
	if err != nil {
		return nil, auditReadError(err)
	}
	out := &governancev1.ListAuthorizationAuditChainResponse{Total: int64(page.Total), PageSize: int32(page.PageSize), Limit: int32(page.Limit), NextCursor: page.NextCursor, HasPrevious: page.HasPrevious}
	for _, e := range page.Entries {
		entry := &governancev1.AuthorizationAuditChainEntry{Sequence: e.Sequence, Id: e.ID, Actor: e.Actor, Action: e.Action, Decision: e.Decision, PreviousDigest: e.PreviousDigest, Digest: e.Digest, ResourceRid: e.ResourceRID, Origin: e.Origin, Payload: e.Payload}
		if len(e.Context) > 0 {
			entry.ContextJson, err = json.Marshal(e.Context)
			if err != nil {
				return nil, auditReadError(err)
			}
		}
		if !e.CreatedAt.IsZero() {
			entry.CreatedAt = timestamppb.New(e.CreatedAt)
			if err = entry.CreatedAt.CheckValid(); err != nil {
				return nil, auditReadError(err)
			}
		}
		out.Entries = append(out.Entries, entry)
	}
	return out, nil
}
