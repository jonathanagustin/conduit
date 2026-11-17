package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	waveapp "github.com/jonathanagustin/conduit/internal/delivery/waves/application"
	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
	wavehttp "github.com/jonathanagustin/conduit/internal/delivery/waves/httpapi"
)

func mountWaves(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	service := &waveapp.Service{Store: repository, Governance: newGovernanceService(repository, options)}
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	wavehttp.Register(mux, wavehttp.Config{
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
		List: service.List,
		Write: func(request *http.Request, body wavehttp.Body) (int, any, error) {
			principal, ok := packagingPrincipal(request, governance, options)
			if !ok {
				return http.StatusUnauthorized, map[string]string{"error": "unauthorized"}, nil
			}
			facts := domain.Facts{Approved: body.Approved, Window: body.WindowOpen, Healthy: map[string]bool{}}
			for _, environment := range body.Healthy {
				facts.Healthy[environment] = true
			}
			var result waveapp.Result
			var err error
			switch body.Action {
			case "create":
				result, err = service.Create(request.Context(), principal.Username, body.ID, body.Product, body.Version, body.Waves)
			case "pause":
				result, err = service.Pause(request.Context(), principal.Username, body.ID, body.Confirm)
			case "start", "resume", "rollback":
				result, err = service.Propose(request.Context(), principal, body.ID, body.Action, facts)
			case "approve":
				result, err = service.Approve(request.Context(), principal, body.ChangeRequestID, body.Rationale)
			default:
				return http.StatusBadRequest, map[string]string{"error": "unknown rollout action"}, nil
			}
			if err != nil {
				return 0, nil, err
			}
			status := http.StatusOK
			if result.ChangeRequestID != "" && body.Action != "approve" {
				status = http.StatusAccepted
			}
			if !result.Allowed && result.ChangeRequestID == "" {
				status = http.StatusConflict
			}
			return status, result, nil
		},
	})
	registerReleaseRollout(mux, repository, options, service.List)
	mux.Handle("/", legacy)
	return mux
}
