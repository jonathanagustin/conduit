package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planninghttp "github.com/jonathanagustin/conduit/internal/planning/httpapi"
)

// mountPlanning exposes the planning-session contract. Turns stay
// unconfigured until a later change wires a planner; compute is refused
// until Accept.
func mountPlanning(legacy http.Handler, options controlplane.HandlerOptions) http.Handler {
	mux := http.NewServeMux()
	planninghttp.Register(mux, planninghttp.Config{
		Service: planningapp.NewService(nil),
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
	})
	mux.Handle("/", legacy)
	return mux
}
