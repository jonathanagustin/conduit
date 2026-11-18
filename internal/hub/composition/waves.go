package composition

import (
	"context"
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
				if rolloutResourcesAllowed(request.Context(), governance, principal, options, rollout, controlplane.OperationReadResource) {
					visible = append(visible, rollout)
				}
			}
			return visible, nil
		},
		Write: func(request *http.Request, body wavehttp.Body) (int, any, error) {
			principal, identityErr := operatorFrom(request.Context())
			if identityErr != nil {
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
				target := domain.Rollout{Product: body.Product, Waves: body.Waves}
				if !rolloutResourcesAllowed(request.Context(), governance, principal, options, target, controlplane.OperationOperate) {
					return http.StatusForbidden, map[string]string{"error": "Product and every target Environment require resource:operate"}, nil
				}
				result, err = service.Create(request.Context(), principal.Username, body.ID, body.Product, body.Version, body.Waves)
			case "pause":
				var target domain.Rollout
				for _, stored := range service.List() {
					if stored.ID == body.ID {
						target = stored
						break
					}
				}
				if !rolloutResourcesAllowed(request.Context(), governance, principal, options, target, controlplane.OperationOperate) {
					return http.StatusForbidden, map[string]string{"error": "Product and every target Environment require resource:operate"}, nil
				}
				result, err = service.Pause(request.Context(), principal.Username, body.ID, body.Confirm)
				if !rolloutResourcesAllowed(request.Context(), governance, principal, options, target, controlplane.OperationReadResource) {
					result.Rollout = domain.Rollout{}
				}
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

// Rollout membership is immutable after creation; every referenced resource must
// authorize the original carrier for the requested operation.
func rolloutResourcesAllowed(ctx context.Context, governance *controlplane.GovernanceService, principal controlplane.Principal, options controlplane.HandlerOptions, rollout domain.Rollout, operation string) bool {
	if rollout.Product == "" || len(rollout.Waves) == 0 || !governance.AuthorizeResourceOperation(ctx, principal, options, "product/"+rollout.Product, operation) {
		return false
	}
	for _, wave := range rollout.Waves {
		if len(wave.Environments) == 0 {
			return false
		}
		for _, environment := range wave.Environments {
			if environment == "" || !governance.AuthorizeResourceOperation(ctx, principal, options, "environment/"+environment, operation) {
				return false
			}
		}
	}
	return true
}
