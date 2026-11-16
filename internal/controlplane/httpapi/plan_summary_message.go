package httpapi

import (
	"errors"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func planSummaryMessage(plan controlplane.PlanSummary) (*plansv1.PlanSummary, error) {
	created, updated := timestamppb.New(plan.CreatedAt), timestamppb.New(plan.UpdatedAt)
	if created.CheckValid() != nil || updated.CheckValid() != nil {
		return nil, errors.New("invalid native Plan timestamp")
	}
	return &plansv1.PlanSummary{Id: nativeIndexString(plan.ID), Environment: nativeIndexString(plan.Environment), Status: nativeIndexString(string(plan.Status)), StepCount: int64(plan.StepCount), ApprovalCount: int64(plan.ApprovalCount), RequiredApprovals: int64(plan.RequiredApprovals), CreatedAt: created, UpdatedAt: updated, Revision: plan.Revision}, nil
}
