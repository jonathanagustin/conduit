package composition

import (
	"errors"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	waveapp "github.com/jonathanagustin/conduit/internal/delivery/waves/application"
	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
	wavehttp "github.com/jonathanagustin/conduit/internal/delivery/waves/httpapi"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
)

func mountWaves(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	service := &waveapp.Service{Store: repository, Governance: newGovernanceService(repository, options)}
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	wavehttp.Register(mux, wavehttp.Config{
		Authorize: func(request *http.Request) error {
			_, err := operatorFrom(request.Context())
			if err == nil {
				return nil
			}
			status := http.StatusInternalServerError
			var identity notificationhttp.StatusError
			if errors.As(err, &identity) {
				status = identity.Status
			}
			return wavehttp.StatusError{Status: status, Message: "operator identity could not be established"}
		},
		List: func(request *http.Request) ([]domain.Rollout, error) {
			principal, err := operatorFrom(request.Context())
			if err != nil {
				return nil, err
			}
			visible := make([]domain.Rollout, 0)
			for _, rollout := range service.List() {
				if rollout.Product == "" || !governance.AuthorizeResourceOperation(request.Context(), principal, options, "product/"+rollout.Product, controlplane.OperationReadResource) {
					continue
				}
				allowed := len(rollout.Waves) > 0
				for _, wave := range rollout.Waves {
					if len(wave.Environments) == 0 {
						allowed = false
					}
					for _, environment := range wave.Environments {
						if environment == "" || !governance.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+environment, controlplane.OperationReadResource) {
							allowed = false
						}
					}
				}
				if allowed {
					visible = append(visible, rollout)
				}
			}
			return visible, nil
		},
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
	return withOperatorCarrier(governance, options, mux)
}
