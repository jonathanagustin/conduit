package composition

import (
	"context"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	cancellationhttp "github.com/jonathanagustin/conduit/internal/controlplane/httpapi"
)

type planCancellationPrincipalKey struct{}

func mountPlanCancellationConnect(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	lifecycle := controlplane.NewLifecycle(repository)
	mux := http.NewServeMux()
	cancellationhttp.RegisterPlanCancellationConnect(mux, cancellationhttp.PlanCancellationConfig{
		Authenticate: func(request *http.Request) (context.Context, error) {
			claims, err := operatorClaims(request, options)
			if err != nil {
				return nil, err
			}
			principal, err := governance.ResolvePrincipal(request.Context(), claims)
			if err != nil {
				return nil, err
			}
			return context.WithValue(request.Context(), planCancellationPrincipalKey{}, principal), nil
		},
		Cancel: func(ctx context.Context, planID string, command controlplane.PlanCancellationCommand) (controlplane.PlanRecord, error) {
			principal, ok := ctx.Value(planCancellationPrincipalKey{}).(controlplane.Principal)
			if !ok {
				return controlplane.PlanRecord{}, controlplane.ErrUnauthorized
			}
			return controlplane.CancelPlanAsOperator(ctx, repository, lifecycle, governance, principal, options, planID, command)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
