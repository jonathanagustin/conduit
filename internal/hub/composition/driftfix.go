package composition

import (
	"net/http"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/driftfix/application"
	driftfixhttp "github.com/jonathanagustin/conduit/internal/delivery/driftfix/httpapi"
)

func mountDriftFix(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	service := application.Service{Store: repository, Governance: newGovernanceService(repository, options)}
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	driftfixhttp.Register(mux, driftfixhttp.Config{
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
		Propose: func(request *http.Request, environment, installation string) (application.Result, error) {
			principal, ok := packagingPrincipal(request, governance, options)
			if !ok {
				return application.Result{}, http.ErrNoCookie
			}
			if !governance.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+strings.TrimSpace(environment), controlplane.OperationProposeChange) {
				return application.Result{}, controlplane.ErrForbidden
			}
			return service.Propose(request.Context(), principal, environment, installation)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
