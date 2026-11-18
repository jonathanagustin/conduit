package composition

import (
	"context"
	"errors"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governanceapp "github.com/jonathanagustin/conduit/internal/governance/application"
	governancehttp "github.com/jonathanagustin/conduit/internal/governance/httpapi"
)

type auditReadPrincipalKey struct{}

func auditCarrierError(err error) error {
	if errors.Is(err, controlplane.ErrUnauthorized) {
		return governanceapp.ErrAuditReadUnauthenticated
	}
	if errors.Is(err, controlplane.ErrForbidden) {
		return governanceapp.ErrAuditReadForbidden
	}
	return err
}

func mountAuthorizationAuditConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	governancehttp.RegisterAuthorizationAuditConnect(mux, governancehttp.AuthorizationAuditConfig{
		Authorize: func(r *http.Request) (context.Context, error) {
			claims, err := operatorClaims(r, options)
			if err != nil {
				return nil, auditCarrierError(err)
			}
			principal, err := governance.ResolvePrincipal(r.Context(), claims)
			if err != nil {
				return nil, auditCarrierError(err)
			}
			if err = controlplane.AuthorizeAuthorizationAuditRead(r.Context(), governance, principal, options); err != nil {
				return nil, auditCarrierError(err)
			}
			return context.WithValue(r.Context(), auditReadPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context, q governanceapp.AuditEventReadQuery) (governanceapp.AuditEventReadPage, error) {
			principal, ok := ctx.Value(auditReadPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return governanceapp.AuditEventReadPage{}, governanceapp.ErrAuditReadUnauthenticated
			}
			return controlplane.ReadAuthorizationAuditEvents(ctx, repository, governance, principal, options, q)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
