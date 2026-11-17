package composition

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	reporting "github.com/jonathanagustin/conduit/internal/reporting/domain"
	reportinghttp "github.com/jonathanagustin/conduit/internal/reporting/httpapi"
)

type overviewPrincipalKey struct{}

func mountReportingConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	reportinghttp.RegisterConnect(mux, reportinghttp.Config{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, overviewIdentityError(err)
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, overviewIdentityError(err)
			}
			return context.WithValue(request.Context(), overviewPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context) (reporting.OverviewProjection, error) {
			principal, ok := ctx.Value(overviewPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return reporting.OverviewProjection{}, controlplane.ErrUnauthorized
			}
			return controlplane.BuildOverviewProjection(ctx, repository, governance, principal, options, time.Now()), nil
		},
	})
	mux.Handle("/", legacy)
	return mux
}

func overviewIdentityError(err error) error {
	if errors.Is(err, controlplane.ErrForbidden) {
		return reportinghttp.ErrPermissionDenied
	}
	if errors.Is(err, controlplane.ErrUnauthorized) {
		return reportinghttp.ErrUnauthenticated
	}
	return err
}
