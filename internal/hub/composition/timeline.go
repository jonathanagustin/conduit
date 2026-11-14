package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	timelineapp "github.com/jonathanagustin/conduit/internal/delivery/timeline/application"
	timelinehttp "github.com/jonathanagustin/conduit/internal/delivery/timeline/httpapi"
)

func mountTimeline(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	mux := http.NewServeMux()
	timelinehttp.Register(mux, timelinehttp.Config{
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
		Build: func(request *http.Request, environment string) (any, error) {
			return timelineapp.Build(request.Context(), repository, environment)
		},
	})
	mux.Handle("/", legacy)
	return mux
}
