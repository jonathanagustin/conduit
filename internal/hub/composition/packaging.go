package composition

import (
	"context"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
)

// mountPackaging puts the packaging-run routes, and the Connect procedure for
// the single-run read, in front of the rest of the Hub. The routes are mounted
// even when no reader is configured so an anonymous caller is refused rather
// than the path not existing. Cancel and rerun authorize and audit before they
// talk to a worker, and neither one cancels a Plan.
func mountPackaging(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, hub Options,
) http.Handler {
	service := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	cfg := packaginghttp.Config{
		Reader: hub.PackagingStatus,
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return packaginghttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			return nil
		},
		Visible: func(request *http.Request, environmentID string) bool {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok || environmentID == "" {
				return false
			}
			return service.AuthorizeResourceOperation(
				request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationReadResource,
			)
		},
		Permit: func(request *http.Request, environmentID string) (packaginghttp.Operator, error) {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok {
				return packaginghttp.Operator{}, packaginghttp.StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			if environmentID == "" || !service.AuthorizeResourceOperation(
				request.Context(), principal, options, "environment/"+environmentID, controlplane.OperationOperate,
			) {
				return packaginghttp.Operator{}, packaginghttp.StatusError{Status: http.StatusForbidden, Message: "forbidden"}
			}
			return packaginghttp.Operator{Username: principal.Username}, nil
		},
		Controls:  hub.PackagingControls,
		Decisions: packagingDecisions{repository: repository},
		Feedback:  packaginghttp.NewMemoryFeedback(),
		OperatorName: func(request *http.Request) string {
			principal, ok := packagingPrincipal(request, service, options)
			if !ok || principal.Username == "" {
				return "operator"
			}
			return principal.Username
		},
	}
	packaginghttp.Register(mux, cfg)
	packaginghttp.RegisterConnect(mux, cfg)
	mux.Handle("/", legacy)
	return mux
}

func packagingPrincipal(
	request *http.Request, service *controlplane.GovernanceService, options controlplane.HandlerOptions,
) (controlplane.Principal, bool) {
	claims, err := operatorClaims(request, options)
	if err != nil {
		return controlplane.Principal{}, false
	}
	principal, err := service.ResolvePrincipal(request.Context(), claims)
	if err != nil {
		return controlplane.Principal{}, false
	}
	return principal, true
}

type packagingDecisions struct {
	repository controlplane.Repository
}

func (log packagingDecisions) Lookup(ctx context.Context, runID, decisionID string) (packaginghttp.DecisionRecord, bool, error) {
	events, err := log.repository.ListAuthorizationAudit(ctx, packagingResource(runID))
	if err != nil {
		return packaginghttp.DecisionRecord{}, false, err
	}
	for _, event := range events {
		if event.Context == nil || event.Context["decisionId"] != decisionID {
			continue
		}
		if event.Action != "packaging-run.cancel" && event.Action != "packaging-run.rerun" {
			continue
		}
		return packaginghttp.DecisionRecord{
			Subject: event.Subject, Action: event.Action, RunID: runID, Verdict: event.Decision,
			Reason: event.Reason, DecisionID: decisionID, At: event.CreatedAt,
		}, true, nil
	}
	return packaginghttp.DecisionRecord{}, false, nil
}

func (log packagingDecisions) Append(ctx context.Context, record packaginghttp.DecisionRecord) error {
	when := record.At
	if when.IsZero() {
		when = time.Now().UTC()
	}
	event := governance.NewAuthorizationCheckEvent(
		when, record.Subject, record.Action, packagingResource(record.RunID), record.Verdict, record.Reason,
		map[string]any{
			"decisionId": record.DecisionID, "sourceDigest": record.SourceDigest,
			"proposalRevision": record.ProposalRevision, "targetPlanRef": record.TargetPlanRef,
			"planId": record.PlanID, "newRunId": record.NewRunID,
		},
	)
	_, err := log.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

func packagingResource(runID string) string { return "packaging-run/" + runID }
