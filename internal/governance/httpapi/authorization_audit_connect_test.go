package httpapi

import (
	"bytes"
	"connectrpc.com/connect/v2"
	"context"
	"encoding/json"
	"errors"
	governancev1 "github.com/jonathanagustin/conduit/gen/go/conduit/governance/v1"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/governance/application"
)

func TestAuditAuthorizationPrecedesMalformedBody(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"anonymous", application.ErrAuditReadUnauthenticated, 401},
		{"non-admin", application.ErrAuditReadForbidden, 403},
		{"backend", errors.New("PRIVATE"), 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			mux := http.NewServeMux()
			RegisterAuthorizationAuditConnect(mux, AuthorizationAuditConfig{
				Authorize: func(r *http.Request) (context.Context, error) { return nil, test.err },
				Read: func(context.Context, application.AuditEventReadQuery) (application.AuditEventReadPage, error) {
					reads++
					return application.AuditEventReadPage{}, nil
				},
			})
			req := httptest.NewRequest("POST", strings.TrimPrefix(AuthorizationAuditConnectRoute, "POST "), strings.NewReader("{broken"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Connect-Protocol-Version", "1")
			out := httptest.NewRecorder()
			mux.ServeHTTP(out, req)
			if out.Code != test.status || reads != 0 || strings.Contains(out.Body.String(), "PRIVATE") {
				t.Fatalf("status=%d reads=%d body=%s", out.Code, reads, out.Body.String())
			}
		})
	}
}

func TestAuditReadPreservesJSONNumbersAndMissingTime(t *testing.T) {
	service := auditReadService{cfg: AuthorizationAuditConfig{Read: func(context.Context, application.AuditEventReadQuery) (application.AuditEventReadPage, error) {
		return application.AuditEventReadPage{Events: []domain.AuthorizationAuditEvent{{ID: "one", RequestFields: map[string]any{"large": json.Number("9007199254740993")}}}, Total: 1, PageSize: 1, Limit: 100}, nil
	}}}
	out, err := service.ListAuthorizationAuditEvents(context.Background(), &governancev1.ListAuthorizationAuditEventsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 || string(out.Events[0].RequestFieldsJson) != `{"large":9007199254740993}` || out.Events[0].CreatedAt != nil || len(out.Events[0].ContextJson) != 0 {
		t.Fatalf("native metadata changed: %v", out)
	}
}

func TestAuditInvalidTimestampPrecedesRead(t *testing.T) {
	reads := 0
	service := auditReadService{cfg: AuthorizationAuditConfig{Read: func(context.Context, application.AuditEventReadQuery) (application.AuditEventReadPage, error) {
		reads++
		return application.AuditEventReadPage{}, nil
	}}}
	_, err := service.ListAuthorizationAuditEvents(context.Background(), &governancev1.ListAuthorizationAuditEventsRequest{Since: &timestamppb.Timestamp{Seconds: 253402300800}})
	if err == nil || reads != 0 {
		t.Fatalf("invalid timestamp reached read: %v reads=%d", err, reads)
	}
}

func TestAuditChainAuthorizationPrecedesMalformedBodyAndRead(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{application.ErrAuditReadUnauthenticated, 401}, {application.ErrAuditReadForbidden, 403}, {errors.New("PRIVATE"), 503},
	} {
		reads := 0
		mux := http.NewServeMux()
		RegisterAuthorizationAuditConnect(mux, AuthorizationAuditConfig{
			Authorize: func(*http.Request) (context.Context, error) { return nil, tc.err },
			ReadChain: func(context.Context, application.AuditChainReadQuery) (application.AuditChainReadPage, error) {
				reads++
				return application.AuditChainReadPage{}, nil
			},
		})
		req := httptest.NewRequest("POST", strings.TrimPrefix(AuthorizationAuditChainConnectRoute, "POST "), strings.NewReader("{broken"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		if out.Code != tc.status || reads != 0 || strings.Contains(out.Body.String(), "PRIVATE") {
			t.Fatalf("status=%d reads=%d body=%s", out.Code, reads, out.Body)
		}
	}
}

func TestAuditChainPreservesExactProjectionMaterial(t *testing.T) {
	payload := []byte{0, 255, 1, 2, 0}
	service := auditReadService{cfg: AuthorizationAuditConfig{ReadChain: func(context.Context, application.AuditChainReadQuery) (application.AuditChainReadPage, error) {
		return application.AuditChainReadPage{Entries: []domain.AuthorizationAuditChainEntry{{ID: "one", Sequence: 9007199254740993, PreviousDigest: "previous", Digest: "digest", Payload: payload, Context: map[string]any{"large": json.Number("9007199254740993")}}}}, nil
	}}}
	out, err := service.ListAuthorizationAuditChain(context.Background(), &governancev1.ListAuthorizationAuditChainRequest{})
	if err != nil {
		t.Fatal(err)
	}
	e := out.Entries[0]
	if e.Sequence != 9007199254740993 || !bytes.Equal(e.Payload, payload) || string(e.ContextJson) != `{"large":9007199254740993}` || e.CreatedAt != nil || e.PreviousDigest != "previous" || e.Digest != "digest" {
		t.Fatalf("projection material changed: %v", e)
	}
}

func TestAuditChainInvalidTimestampAndSanitizedBackend(t *testing.T) {
	reads := 0
	service := auditReadService{cfg: AuthorizationAuditConfig{ReadChain: func(context.Context, application.AuditChainReadQuery) (application.AuditChainReadPage, error) {
		reads++
		return application.AuditChainReadPage{}, errors.New("PRIVATE")
	}}}
	_, err := service.ListAuthorizationAuditChain(context.Background(), &governancev1.ListAuthorizationAuditChainRequest{Since: &timestamppb.Timestamp{Seconds: 253402300800}})
	if connect.CodeOf(err) != connect.CodeInvalidArgument || reads != 0 {
		t.Fatalf("invalid timestamp reached read: %v %d", err, reads)
	}
	_, err = service.ListAuthorizationAuditChain(context.Background(), &governancev1.ListAuthorizationAuditChainRequest{})
	if connect.CodeOf(err) != connect.CodeUnavailable || strings.Contains(err.Error(), "PRIVATE") || reads != 1 {
		t.Fatalf("backend error leaked: %v %d", err, reads)
	}
}
