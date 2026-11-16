package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

// AcceptAuditor persists the acceptance receipt for a planning session.
type AcceptAuditor interface {
	RecordAccept(ctx context.Context, actor string, record application.Audit) error
}

// ActorPlanner wraps a planningapp.Planner so the authenticated operator
// identity is propagated to the underlying model audit logger.
type ActorPlanner interface {
	TurnForActor(ctx context.Context, actor string, session *domain.Session, message string) (*application.TurnResult, error)
}

// Activities holds the outbound ports invoked by SessionWorkflow.
type Activities struct {
	Planner      application.Planner
	ActorPlanner ActorPlanner
	Auditor      AcceptAuditor
}

type heartbeatToolObserver struct {
	actCtx context.Context
	seq    atomic.Uint64
}

func (h *heartbeatToolObserver) Started(_ context.Context, name string, _ json.RawMessage) (string, error) {
	callID := fmt.Sprintf("call-%d", h.seq.Add(1))
	activity.RecordHeartbeat(h.actCtx, map[string]string{
		"phase":  "tool_started",
		"callId": callID,
		"tool":   name,
	})
	return callID, nil
}

func (h *heartbeatToolObserver) Finished(_ context.Context, callID string, succeeded bool) error {
	activity.RecordHeartbeat(h.actCtx, map[string]string{
		"phase":  "tool_finished",
		"callId": callID,
		"ok":     boolText(succeeded),
	})
	return nil
}

// TurnActivity executes a single planning turn (repository clone, inspect_repo,
// read_file, Needs Input questions, or plan synthesis) with Temporal activity
// heartbeating, structured logging, and metrics.
func (a Activities) TurnActivity(ctx context.Context, in TurnActivityInput) (*application.TurnResult, error) {
	logger := activity.GetLogger(ctx)
	metrics := activity.GetMetricsHandler(ctx)
	start := time.Now()
	if in.Session == nil {
		return nil, temporal.NewNonRetryableApplicationError("planning session is required", ErrTypeBadInput, nil)
	}
	if a.Planner == nil && a.ActorPlanner == nil {
		return nil, temporal.NewNonRetryableApplicationError(domain.ErrModelNotConfigured.Error(), ErrTypeModelNotConfigured, nil)
	}

	logger.Info("planning turn activity started",
		"sessionId", in.Session.ID,
		"actor", in.Actor,
		"workflowId", in.WorkflowID,
		"runId", in.RunID,
	)
	activity.RecordHeartbeat(ctx, map[string]string{
		"phase":     "start",
		"sessionId": in.Session.ID,
	})

	runCtx := application.WithRunScope(ctx, application.RunScope{
		ThreadID: in.Session.ID,
		RunID:    in.RunID,
		Tools:    &heartbeatToolObserver{actCtx: ctx},
	})

	var (
		res *application.TurnResult
		err error
	)
	if a.ActorPlanner != nil {
		res, err = a.ActorPlanner.TurnForActor(runCtx, in.Actor, in.Session, in.Message)
	} else {
		res, err = a.Planner.Turn(runCtx, in.Session, in.Message)
	}
	elapsed := time.Since(start)
	metrics.Timer("conduit_planning_activity_duration_ms").Record(elapsed)
	if err != nil {
		metrics.WithTags(map[string]string{"outcome": "fail"}).Counter(MetricPlanningActivityCalls).Inc(1)
		logger.Warn("planning turn activity failed",
			"sessionId", in.Session.ID,
			"durationMs", elapsed.Milliseconds(),
			"error", err,
		)
		if errors.Is(err, domain.ErrModelNotConfigured) {
			return nil, temporal.NewNonRetryableApplicationError(err.Error(), ErrTypeModelNotConfigured, err)
		}
		var intakeErr domain.IntakeError
		if errors.As(err, &intakeErr) {
			return nil, temporal.NewNonRetryableApplicationError(err.Error(), ErrTypeBadInput, err)
		}
		return nil, err
	}
	metrics.WithTags(map[string]string{"outcome": "pass"}).Counter(MetricPlanningActivityCalls).Inc(1)
	if res == nil {
		res = &application.TurnResult{}
	}
	if strings.TrimSpace(res.Model) == "" {
		res.Model = domain.DefaultModelLabel
	}
	logger.Info("planning turn activity completed",
		"sessionId", in.Session.ID,
		"durationMs", elapsed.Milliseconds(),
		"model", res.Model,
		"hasPlan", res.Plan != nil,
		"questions", len(res.NeedsInput),
	)
	return res, nil
}

// AuditAcceptActivity writes the governance acceptance receipt before the
// workflow transitions the session to StatusAccepted.
func (a Activities) AuditAcceptActivity(ctx context.Context, in AuditAcceptActivityInput) error {
	logger := activity.GetLogger(ctx)
	metrics := activity.GetMetricsHandler(ctx)
	if a.Auditor == nil {
		return nil
	}
	if err := a.Auditor.RecordAccept(ctx, in.Actor, in.Record); err != nil {
		metrics.WithTags(map[string]string{"outcome": "fail"}).Counter(MetricPlanningActivityCalls).Inc(1)
		logger.Error("planning accept audit failed", "sessionId", in.Session, "actor", in.Actor, "error", err)
		return temporal.NewNonRetryableApplicationError(err.Error(), ErrTypeAuditFailed, err)
	}
	metrics.WithTags(map[string]string{"outcome": "pass"}).Counter(MetricPlanningActivityCalls).Inc(1)
	logger.Info("planning accept audit committed",
		"sessionId", in.Session,
		"actor", in.Actor,
		"planHash", in.Record.PlanHash,
	)
	return nil
}

// RegisterWorker registers SessionWorkflow and its activities on a Temporal worker.
func RegisterWorker(c client.Client, queue string, acts Activities) worker.Worker {
	if strings.TrimSpace(queue) == "" {
		queue = TaskQueue
	}
	w := worker.New(c, queue, worker.Options{})
	RegisterOnWorker(w, acts)
	return w
}

// RegisterOnWorker registers SessionWorkflow and its activities onto an existing worker.Registry.
func RegisterOnWorker(w worker.Registry, acts Activities) {
	w.RegisterWorkflowWithOptions(SessionWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	w.RegisterActivityWithOptions(acts.TurnActivity, activity.RegisterOptions{Name: ActTurn})
	w.RegisterActivityWithOptions(acts.AuditAcceptActivity, activity.RegisterOptions{Name: ActAuditAccept})
}

func boolText(ok bool) string {
	if ok {
		return "true"
	}
	return "false"
}
