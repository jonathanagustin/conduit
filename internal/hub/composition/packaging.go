package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

// mountPackaging puts the read-only packaging-run routes in front of the rest
// of the Hub. The routes are mounted even when no reader is configured so an
// anonymous caller is refused rather than the path not existing. They have no
// write method: approval stays on the governance routes.
func mountPackaging(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, hub Options,
) http.Handler {
	service := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	packaginghttp.Register(mux, packaginghttp.Config{
		Reader: hub.PackagingStatus,
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return packaginghttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			return nil
		},
		Visible: func(request *http.Request, environmentID string) bool {
			if environmentID == "" {
				return false
			}
			claims, err := operatorClaims(request, options)
			if err != nil {
				return false
			}
			principal, err := service.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return false
			}
			return service.AuthorizeResourceOperation(
				request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationReadResource,
			)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
