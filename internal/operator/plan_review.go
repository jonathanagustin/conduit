package operator

import (
	"context"
	"errors"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	review "github.com/jonathanagustin/conduit/internal/planreview/domain"
)

type ReviewedPlanControlPlane interface {
	ProposeReviewedPlan(context.Context, controlplane.DesiredState, review.Review) (controlplane.PlanRecord, error)
}

func (service Service) ProposeReviewedPlan(ctx context.Context, desired controlplane.DesiredState, context review.Review) (controlplane.PlanRecord, error) {
	port, ok := service.controlPlane.(ReviewedPlanControlPlane)
	if !ok {
		return controlplane.PlanRecord{}, errors.New("reviewed Plan proposals are unavailable from this service")
	}
	return port.ProposeReviewedPlan(ctx, desired, context)
}

func (adapter *HTTPControlPlane) ProposeReviewedPlan(ctx context.Context, desired controlplane.DesiredState, context review.Review) (controlplane.PlanRecord, error) {
	if err := context.Validate(); err != nil {
		return controlplane.PlanRecord{}, err
	}
	var result controlplane.PlanRecord
	body := struct {
		DesiredState controlplane.DesiredState `json:"desiredState"`
		Review       review.Review             `json:"review"`
	}{desired, context}
	err := adapter.requestWithMediaType(ctx, http.MethodPost, "/v1/plans", body, &result, review.MediaType)
	return result, err
}
