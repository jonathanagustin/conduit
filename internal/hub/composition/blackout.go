package composition

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	blackoutapp "github.com/jonathanagustin/conduit/internal/delivery/blackout/application"
	blackouthttp "github.com/jonathanagustin/conduit/internal/delivery/blackout/httpapi"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
)

func mountBlackouts(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	service := &blackoutapp.Service{Store: repository, Governance: newGovernanceService(repository, options)}
	governance := newGovernanceService(repository, options)
	mux := http.NewServeMux()
	blackouthttp.Register(mux, blackouthttp.Config{
		Authorize: func(request *http.Request) error {
			principal, err := operatorFrom(request.Context())
			if err != nil {
				status := http.StatusInternalServerError
				var identity notificationhttp.StatusError
				if errors.As(err, &identity) {
					status = identity.Status
				}
				return blackouthttp.StatusError{Status: status, Message: "operator identity could not be established"}
			}
			if request.Method == http.MethodGet {
				query, err := url.ParseQuery(request.URL.RawQuery)
				values := query["environment"]
				if err != nil || len(values) != 1 || values[0] == "" {
					return blackouthttp.StatusError{Status: http.StatusBadRequest, Message: "one Environment identity is required"}
				}
				if !governance.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+values[0], controlplane.OperationReadResource) {
					return blackouthttp.StatusError{Status: http.StatusForbidden, Message: "Environment read permission is required"}
				}
			}
			return nil
		},
		Active:   service.Active,
		Snapshot: service.Snapshot,
		Write: func(request *http.Request) (int, any, error) {
			principal, err := operatorFrom(request.Context())
			if err != nil {
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
				if !governance.AuthorizeResourceOperation(request.Context(), principal, options, "environment/"+body.Environment, controlplane.OperationOperate) {
					return http.StatusForbidden, map[string]string{"error": "Environment resource:operate permission is required"}, nil
				}
				result, err := service.Declare(request.Context(), principal.Username, body.Environment, body.Reason, body.StartsAt, body.EndsAt, body.Confirm)
				if err != nil {
					return 0, nil, err
				}
				// Return the new declaration receipt, never historical intervals.
				if result.Allowed && len(result.Blackouts) > 0 {
					result.Blackouts = result.Blackouts[len(result.Blackouts)-1:]
				} else {
					result.Blackouts = nil
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
	return withOperatorCarrier(governance, options, mux)
}
