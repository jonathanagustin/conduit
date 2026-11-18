package operator

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	packagingv1 "github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1"
	"github.com/jonathanagustin/conduit/gen/go/conduit/packaging/v1/packagingv1connect"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

// GetPackagingRun reads the existing native status projection through the
// explicitly selected transport. A failure is never replayed over REST.
func (adapter *HTTPControlPlane) GetPackagingRun(ctx context.Context, id string) (domain.RunStatus, error) {
	if id == "" || strings.TrimSpace(id) != id || !utf8.ValidString(id) {
		return domain.RunStatus{}, fmt.Errorf("packaging run id must be nonempty and unpadded")
	}
	var row domain.RunStatus
	if adapter.packagingReadTransport == "connect" {
		client := packagingv1connect.NewPackagingRunServiceClient(connect.NewClient(connecthttp.NewTransport(adapter.client, adapter.baseURL.String(), connecthttp.WithReadMaxBytes(maxOperatorResponseBytes))))
		response, err := client.GetPackagingRun(ctx, &packagingv1.GetPackagingRunRequest{RunId: id})
		if err != nil {
			return domain.RunStatus{}, err
		}
		if response == nil {
			return domain.RunStatus{}, fmt.Errorf("operator control plane returned no packaging status")
		}
		row = domain.RunStatus{
			Tier: response.Tier, Configured: response.Configured, Authority: response.Authority,
			PlanAuthority: response.PlanAuthority, ApprovalAuthority: response.ApprovalAuthority,
			RunID: response.RunId, Phase: response.Phase, Outcome: response.Outcome, Reason: response.Reason,
			SourceDigest: response.SourceDigest, ArtifactDigest: response.ArtifactDigest,
			ProposalRevision: response.ProposalRevision, TargetPlanRef: response.TargetPlanRef, PlanID: response.PlanId,
			HandoffReconciled: response.HandoffReconciled, LastReview: response.LastReview,
			DuplicateReviews: int(response.DuplicateReviews), TemplateVersion: response.TemplateVersion,
			ToolVersions: response.ToolVersions, AttachmentSHA256: append([]string(nil), response.AttachmentSha256...),
		}
	} else if err := adapter.request(ctx, http.MethodGet, "/v1/packaging-runs/"+url.PathEscape(id), nil, &row); err != nil {
		return domain.RunStatus{}, err
	}
	if (row.RunID != id && (row.Configured || row.RunID != "")) || row.DuplicateReviews < 0 || row.Tier != "CONDUIT_NATIVE" || row.Authority != "packaging-run" || row.PlanAuthority != "conduit-plan" || row.ApprovalAuthority != "conduit-governance" {
		return domain.RunStatus{}, fmt.Errorf("operator control plane returned mismatched or invalid packaging status")
	}
	return row, nil
}
