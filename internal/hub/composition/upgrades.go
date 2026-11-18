package composition

import (
	"errors"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	upgradeapp "github.com/jonathanagustin/conduit/internal/delivery/upgrades/application"
	upgradehttp "github.com/jonathanagustin/conduit/internal/delivery/upgrades/httpapi"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
)

func mountUpgrades(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	upgradehttp.Register(mux, upgradehttp.Config{
		Authorize: func(request *http.Request) error {
			_, err := operatorFrom(request.Context())
			return upgradeIdentityError(err)
		},
		Select: func(w http.ResponseWriter, request *http.Request) (func(string) bool, bool) {
			principal, err := operatorFrom(request.Context())
			if err != nil {
				http.Error(w, "operator identity could not be established", http.StatusUnauthorized)
				return nil, false
			}
			return controlplane.ResolveSpaceEnvironmentFilter(w, request, principal, options)
		},
		Dependencies: func(request *http.Request, product, version string) (any, error) {
			principal, err := operatorFrom(request.Context())
			if err != nil {
				return nil, upgradeIdentityError(err)
			}
			if !governance.AuthorizeResourceOperation(request.Context(), principal, options, "product/"+product, controlplane.OperationReadResource) {
				return nil, upgradehttp.StatusError{Status: http.StatusForbidden, Message: "Product read permission is required"}
			}
			return upgradeapp.Dependencies(request.Context(), repository, product, version)
		},
		Upgrades: func(request *http.Request, selected func(string) bool) (any, error) {
			principal, err := operatorFrom(request.Context())
			if err != nil {
				return nil, upgradeIdentityError(err)
			}
			rows, err := upgradeapp.Behind(request.Context(), repository, map[string]string{})
			if err != nil {
				return nil, err
			}
			visible := make([]upgradeapp.Upgrade, 0, len(rows))
			for _, row := range rows {
				if selected != nil && !selected(row.Environment) {
					continue
				}
				if governance.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+row.Environment, controlplane.OperationReadResource) {
					visible = append(visible, row)
				}
			}
			return visible, nil
		},
	})
	mux.Handle("/", legacy)
	return withOperatorCarrier(governance, options, mux)
}

func upgradeIdentityError(err error) error {
	if err == nil {
		return nil
	}
	status := http.StatusInternalServerError
	var identity notificationhttp.StatusError
	if errors.As(err, &identity) {
		status = identity.Status
	} else if errors.Is(err, controlplane.ErrUnauthorized) {
		status = http.StatusUnauthorized
	} else if errors.Is(err, controlplane.ErrForbidden) {
		status = http.StatusForbidden
	}
	return upgradehttp.StatusError{Status: status, Message: "operator identity could not be established"}
}
