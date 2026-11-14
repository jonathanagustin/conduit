package composition

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	blackoutapp "github.com/jonathanagustin/conduit/internal/delivery/blackout/application"
	blackouthttp "github.com/jonathanagustin/conduit/internal/delivery/blackout/httpapi"
)

func mountBlackouts(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	service := &blackoutapp.Service{Store: repository}
	governance := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	blackouthttp.Register(mux, blackouthttp.Config{
		Authorize: func(request *http.Request) error {
			_, err := operatorClaims(request, options)
			return err
		},
		Active: service.Active,
		Write: func(request *http.Request) (int, any, error) {
			principal, ok := packagingPrincipal(request, governance, options)
			if !ok {
				return http.StatusUnauthorized, map[string]string{"error": "unauthorized"}, nil
			}
			var body struct {
				Action      string    `json:"action"`
				Environment string    `json:"environment"`
				Reason      string    `json:"reason"`
				StartsAt    time.Time `json:"startsAt"`
				EndsAt      time.Time `json:"endsAt"`
				Confirm     bool      `json:"confirm"`
				ChangeID    string    `json:"changeRequestId"`
				Rationale   string    `json:"rationale"`
			}
			if err := json.NewDecoder(io.LimitReader(request.Body, 1<<16)).Decode(&body); err != nil {
				return http.StatusBadRequest, map[string]string{"error": "request body is not valid JSON"}, nil
			}
			switch body.Action {
			case "declare":
				result, err := service.Declare(request.Context(), principal.Username, body.Environment, body.Reason, body.StartsAt, body.EndsAt, body.Confirm)
				if err != nil {
					return 0, nil, err
				}
				status := http.StatusCreated
				if !result.Allowed {
					status = http.StatusConflict
				}
				return status, result, nil
			case "clear":
				result, err := service.ProposeClear(request.Context(), principal, body.Environment)
				if err != nil {
					return 0, nil, err
				}
				return http.StatusAccepted, result, nil
			case "approve":
				result, err := service.ApproveClear(request.Context(), principal, body.ChangeID, body.Rationale)
				if err != nil {
					return 0, nil, err
				}
				status := http.StatusOK
				if !result.Allowed {
					status = http.StatusConflict
				}
				return status, result, nil
			default:
				return http.StatusBadRequest, map[string]string{"error": "unknown blackout action"}, nil
			}
		},
	})
	mux.Handle("/", legacy)
	return mux
}
