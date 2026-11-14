package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	upgradeapp "github.com/jonathanagustin/conduit/internal/delivery/upgrades/application"
	upgradehttp "github.com/jonathanagustin/conduit/internal/delivery/upgrades/httpapi"
)

func mountUpgrades(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	mux := http.NewServeMux()
	upgradehttp.Register(mux, upgradehttp.Config{
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
		Dependencies: func(request *http.Request, product, version string) (any, error) {
			return upgradeapp.Dependencies(request.Context(), repository, product, version)
		},
		Upgrades: func(request *http.Request) (any, error) {
			return upgradeapp.Behind(request.Context(), repository, map[string]string{})
		},
	})
	mux.Handle("/", legacy)
	return mux
}
