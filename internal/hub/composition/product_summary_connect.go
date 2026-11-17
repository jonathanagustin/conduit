package composition

import (
	"context"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	producthttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type productSummaryPrincipalKey struct{}

func mountProductSummaryConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	producthttp.RegisterProductSummaryConnect(mux, producthttp.ProductSummaryConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), productSummaryPrincipalKey{}, principal), nil
		},
		Read: func(ctx context.Context, search string, cursor string, limit int) (controlplane.ProductCatalogSummaryPage, error) {
			principal, ok := ctx.Value(productSummaryPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.ProductCatalogSummaryPage{}, controlplane.ErrUnauthorized
			}
			return controlplane.ReadAuthorizedProductSummaryPage(ctx, repository, governance, principal, options, search, cursor, limit)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
