package composition

import (
	"net/http"
	"os"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/checkin/adapters/local"
	"github.com/jonathanagustin/conduit/internal/delivery/checkin/adapters/recorded"
	checkinapp "github.com/jonathanagustin/conduit/internal/delivery/checkin/application"
	checkinhttp "github.com/jonathanagustin/conduit/internal/delivery/checkin/httpapi"
	"github.com/jonathanagustin/conduit/internal/delivery/checkin/ports"
	"time"
)

func mountSiteCheckIn(legacy http.Handler, options controlplane.HandlerOptions) http.Handler {
	service := newCheckInService()
	mux := http.NewServeMux()
	checkinhttp.Register(mux, checkinhttp.Config{
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
		Observe: func(request *http.Request, site, environment string, reportOld bool) (checkinapp.Status, error) {
			return service.Observe(request.Context(), site, environment, reportOld)
		},
		Submit: service.Submit,
	})
	mux.Handle("/", legacy)
	return mux
}

func newCheckInService() *checkinapp.Service {
	if os.Getenv("CONDUIT_CHECKIN") == "local" {
		return &checkinapp.Service{Checker: local.Adapter{State: ports.Online}}
	}
	rows := map[string]ports.CheckIn{}
	if os.Getenv("DEMO_FLEET") == "1" {
		at := time.Now().UTC()
		rows["east"] = ports.CheckIn{Site: "east", Environment: "east", State: ports.Online, At: at}
		rows["north"] = ports.CheckIn{Site: "north", Environment: "north", State: ports.Disconnected, At: at}
	}
	return &checkinapp.Service{Checker: recorded.Adapter{Rows: rows}}
}
