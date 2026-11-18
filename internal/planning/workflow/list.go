package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	workflowpb "go.temporal.io/api/workflow/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type visibilityClient interface {
	ListWorkflow(context.Context, *workflowservice.ListWorkflowExecutionsRequest) (*workflowservice.ListWorkflowExecutionsResponse, error)
}

// List reads Temporal visibility and then verifies ownership against the exact
// selected execution before reading any session payload. Visibility is eventual
// and retention-bounded; this is not a linearizable or permanent-history index.
func (s *ClientService) List(ctx context.Context) ([]domain.SessionSummary, error) {
	visibility, ok := s.client.(visibilityClient)
	if !ok || strings.TrimSpace(s.actor) == "" {
		return nil, errors.New("owned planning visibility is not configured")
	}
	latest := make(map[string]*workflowpb.WorkflowExecutionInfo)
	tokens := make(map[string]bool)
	var token []byte
	scanned := 0
	for pages := 0; ; pages++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if pages >= domain.MaxIndexedExecutions {
			return nil, domain.ErrIndexTooLarge
		}
		response, err := visibility.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
			PageSize: 200, NextPageToken: token, Query: `WorkflowType = "planning.Session"`,
		})
		if err != nil {
			return nil, fmt.Errorf("read planning visibility: %w", err)
		}
		if response == nil {
			return nil, errors.New("planning visibility returned no authoritative response")
		}
		for _, execution := range response.Executions {
			scanned++
			if scanned > domain.MaxIndexedExecutions {
				return nil, domain.ErrIndexTooLarge
			}
			id, run := execution.GetExecution().GetWorkflowId(), execution.GetExecution().GetRunId()
			if (!strings.HasPrefix(id, WorkflowID("")) || strings.TrimPrefix(id, WorkflowID("")) == "") || run == "" || (execution.GetStartTime() == nil || execution.GetStartTime().CheckValid() != nil) || execution.GetType().GetName() != WorkflowName {
				return nil, errors.New("planning visibility returned an invalid execution")
			}
			prior := latest[id]
			if prior == nil || execution.GetStartTime().AsTime().After(prior.GetStartTime().AsTime()) ||
				(execution.GetStartTime().AsTime().Equal(prior.GetStartTime().AsTime()) && run > prior.GetExecution().GetRunId()) {
				latest[id] = execution
			}
		}
		token = response.NextPageToken
		if len(token) == 0 {
			break
		}
		if tokens[string(token)] {
			return nil, errors.New("planning visibility repeated its continuation token")
		}
		tokens[string(token)] = true
	}
	ids := make([]string, 0, len(latest))
	for id := range latest {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]domain.SessionSummary, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		run := latest[id].GetExecution().GetRunId()
		ownerValue, err := s.client.QueryWorkflow(ctx, id, run, QueryOwner)
		if err != nil {
			return nil, fmt.Errorf("verify planning inventory owner: %w", err)
		}
		if ownerValue == nil {
			return nil, errors.New("planning owner query returned no value")
		}
		var owner string
		if err := ownerValue.Get(&owner); err != nil {
			return nil, err
		}
		// Legacy ownerless sessions are not a shared catalog. Never fetch their payload.
		if owner != s.actor {
			continue
		}
		value, err := s.client.QueryWorkflow(ctx, id, run, QuerySession)
		if err != nil {
			return nil, fmt.Errorf("read owned planning summary: %w", err)
		}
		if value == nil {
			return nil, errors.New("planning session query returned no value")
		}
		var session domain.Session
		if err := value.Get(&session); err != nil {
			return nil, err
		}
		if session.ID != strings.TrimPrefix(id, "planning-session-") {
			return nil, errors.New("planning inventory session identity mismatch")
		}
		summary := domain.SummarizeSession(&session)
		summary.WorkflowRunID = run
		items = append(items, summary)
	}
	return items, nil
}
