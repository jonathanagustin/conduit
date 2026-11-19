package operator

import (
	"context"
	"fmt"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	plansv1 "github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/plans/v1/plansv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// SetPlanReadTransport selects only the summary index read, never a command.
func (adapter *HTTPControlPlane) SetPlanReadTransport(mode string) error {
	if mode != "" && mode != "rest" && mode != "connect" {
		return fmt.Errorf("plan read transport must be rest or connect")
	}
	adapter.planReadTransport = mode
	return nil
}

func (adapter *HTTPControlPlane) listPlanSummariesConnect(ctx context.Context, query PlanListQuery) (PlanListResult, error) {
	// Capture selection for every page and response binding in this operation.
	spaceID := adapter.spaceID
	transport := connect.NewClient(connecthttp.NewTransport(adapter.client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes)))
	client := plansv1connect.NewPlanReadServiceClient(transport)
	scoped := plansv1connect.NewPlanScopedReadServiceClient(transport)
	limit := int32(200)
	request := &plansv1.ListPlanSummariesRequest{Limit: &limit, Environment: query.Environment, Status: query.Status, InstallationId: query.InstallationID, Search: query.Search}
	result := PlanListResult{Plans: []controlplane.PlanSummary{}}
	seen := map[string]bool{}
	maximum := int64(^uint(0) >> 1)
	for {
		var page *plansv1.ListPlanSummariesResponse
		if spaceID != "" {
			response, err := scoped.ListPlanSummariesInSpace(ctx, &plansv1.ListPlanSummariesInSpaceRequest{SpaceId: spaceID, Limit: request.Limit, Cursor: request.Cursor, Environment: request.Environment, Status: request.Status, InstallationId: request.InstallationId, Search: request.Search})
			if err != nil {
				return PlanListResult{}, err
			}
			if response == nil || response.SpaceId != spaceID || response.Page == nil {
				return PlanListResult{}, fmt.Errorf("operator control plane returned an invalid scoped Plan summary selection")
			}
			page = response.Page
		} else {
			var err error
			page, err = client.ListPlanSummaries(ctx, request)
			if err != nil {
				return PlanListResult{}, err
			}
		}
		if page == nil {
			return PlanListResult{}, fmt.Errorf("operator control plane returned an invalid Plan summary page")
		}
		if page.Total < int64(len(page.Plans)) || page.Total > maximum || page.Limit < 1 || page.Limit > 200 || len(page.Plans) > int(page.Limit) || int(page.PageSize) != len(page.Plans) {
			return PlanListResult{}, fmt.Errorf("operator control plane returned an invalid Plan summary page")
		}
		result.Total = int(page.Total)
		for _, plan := range page.Plans {
			if plan == nil || plan.Id == "" || plan.CreatedAt == nil || plan.UpdatedAt == nil || plan.CreatedAt.CheckValid() != nil || plan.UpdatedAt.CheckValid() != nil {
				return PlanListResult{}, fmt.Errorf("operator control plane returned an invalid Plan summary row")
			}
			for _, count := range []int64{plan.StepCount, plan.ApprovalCount, plan.RequiredApprovals} {
				if count > maximum || count < -maximum-1 {
					return PlanListResult{}, fmt.Errorf("operator control plane returned an unrepresentable Plan summary count")
				}
			}
			result.Plans = append(result.Plans, controlplane.PlanSummary{
				ID: plan.Id, Environment: plan.Environment, Status: controlplane.PlanStatus(plan.Status),
				StepCount: int(plan.StepCount), ApprovalCount: int(plan.ApprovalCount), RequiredApprovals: int(plan.RequiredApprovals),
				CreatedAt: plan.CreatedAt.AsTime(), UpdatedAt: plan.UpdatedAt.AsTime(), Revision: plan.Revision,
			})
		}
		if page.NextCursor == "" {
			return result, nil
		}
		if seen[page.NextCursor] {
			return PlanListResult{}, fmt.Errorf("operator control plane repeated a page cursor")
		}
		seen[page.NextCursor] = true
		request.Cursor = page.NextCursor
	}
}
