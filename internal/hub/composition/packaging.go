package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

// mountPackaging puts the read-only packaging-run route in front of the rest
// of the Hub. The route is mounted even when no reader is configured so an
// anonymous caller is refused rather than the path not existing. It has no
// write method: approval stays on the governance routes.
func mountPackaging(legacy http.Handler, options controlplane.HandlerOptions, hub Options) http.Handler {
	mux := http.NewServeMux()
	packaginghttp.Register(mux, packaginghttp.Config{
		Reader: hub.PackagingStatus,
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return packaginghttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			return nil
		},
	})
	mux.Handle("/", legacy)
	return mux
}
