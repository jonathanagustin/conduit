package operator

import (
	"context"
	"fmt"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1/packagingv1connect"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// SetPackagingReadTransport selects packaging index and detail reads. There is no
// automatic replay through another transport on failure.
func (adapter *HTTPControlPlane) SetPackagingReadTransport(mode string) error {
	if mode != "" && mode != "rest" && mode != "connect" {
		return fmt.Errorf("packaging read transport must be rest or connect")
	}
	adapter.packagingReadTransport = mode
	return nil
}

func (adapter *HTTPControlPlane) listPackagingRunsConnect(ctx context.Context, query PackagingRunQuery) (PackagingRunList, error) {
	client := packagingv1connect.NewPackagingRunServiceClient(connect.NewClient(connecthttp.NewTransport(adapter.client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes))))
	limit := int32(200)
	request := &packagingv1.ListPackagingRunsRequest{View: "summary", Limit: &limit, Phase: query.Phase, Outcome: query.Outcome, PlanId: query.PlanID, Search: query.Search}
	result := PackagingRunList{Items: []domain.RunStatus{}}
	seen := map[string]bool{}
	first := true
	for {
		page, err := client.ListPackagingRuns(ctx, request)
		if err != nil {
			return PackagingRunList{}, err
		}
		if page.Total < int64(len(page.Items)) || page.Limit < 1 || page.Limit > 200 || len(page.Items) > int(page.Limit) || uint64(page.Total) > uint64(^uint(0)>>1) || int(page.PageSize) != len(page.Items) || len(page.Items) > 200 {
			return PackagingRunList{}, fmt.Errorf("operator control plane returned an invalid packaging page")
		}
		if first {
			result.Tier, result.Configured, result.Authority = page.Tier, page.Configured, page.Authority
			result.PlanAuthority, result.ApprovalAuthority = page.PlanAuthority, page.ApprovalAuthority
			first = false
		}
		result.Total = int(page.Total)
		for _, run := range page.Items {
			if run == nil || run.RunId == "" {
				return PackagingRunList{}, fmt.Errorf("operator control plane returned an invalid packaging row")
			}
			result.Items = append(result.Items, domain.RunStatus{
				Tier: run.Tier, Configured: run.Configured, Authority: run.Authority, PlanAuthority: run.PlanAuthority, ApprovalAuthority: run.ApprovalAuthority,
				RunID: run.RunId, Phase: run.Phase, Outcome: run.Outcome, Reason: run.Reason, SourceDigest: run.SourceDigest, ArtifactDigest: run.ArtifactDigest,
				ProposalRevision: run.ProposalRevision, TargetPlanRef: run.TargetPlanRef, PlanID: run.PlanId, HandoffReconciled: run.HandoffReconciled,
				LastReview: run.LastReview, DuplicateReviews: int(run.DuplicateReviews), TemplateVersion: run.TemplateVersion, ToolVersions: run.ToolVersions,
				AttachmentSHA256: append([]string(nil), run.AttachmentSha256...),
			})
		}
		if page.NextCursor == "" {
			return result, nil
		}
		if seen[page.NextCursor] {
			return PackagingRunList{}, fmt.Errorf("operator control plane repeated a page cursor")
		}
		seen[page.NextCursor] = true
		request.Cursor = page.NextCursor
	}
}
