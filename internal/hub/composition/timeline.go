package composition

import (
	"errors"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	timelineapp "github.com/jonathanagustin/conduit/internal/delivery/timeline/application"
	timelinehttp "github.com/jonathanagustin/conduit/internal/delivery/timeline/httpapi"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
)

func mountTimeline(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	timelinehttp.Register(mux, timelinehttp.Config{
		Authorize: func(request *http.Request) error {
			principal, err := operatorFrom(request.Context())
			if err != nil {
				status := http.StatusInternalServerError
				var identity notificationhttp.StatusError
				if errors.As(err, &identity) {
					status = identity.Status
				}
				return timelinehttp.StatusError{Status: status, Message: "operator identity could not be established"}
			}
			if !governance.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+request.PathValue("environmentID"), controlplane.OperationReadResource) {
				return timelinehttp.StatusError{Status: http.StatusForbidden, Message: "Environment read permission is required"}
			}
			return nil
		},
		Build: func(request *http.Request, environment string) (any, error) {
			return timelineapp.Build(request.Context(), repository, environment)
		},
	})
	mux.Handle("/", legacy)
	return withOperatorCarrier(governance, options, mux)
}
