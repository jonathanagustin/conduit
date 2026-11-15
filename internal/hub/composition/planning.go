package composition

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/agentstream"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	planninghttp "github.com/jonathanagustin/conduit/internal/planning/httpapi"
)

// mountPlanning exposes the planning-session contract. When
// CONDUIT_MODEL_CMD is set, turns inspect the intake repository through
// inspect_repo and read_file. Compute is refused until Accept.
func mountPlanning(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) (http.Handler, error) {
	runner, err := planningPlanner(os.Getenv, repository)
	if err != nil {
		return nil, err
	}
	return planningHandler(legacy, repository, options, runner), nil
}

func planningHandler(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, runner planningapp.Planner) http.Handler {
	var stores sync.Map
	governanceService := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	type ownerServices struct {
		sessions *planningapp.Service
		runs     *agentstream.Service
	}
	resolve := func(request *http.Request) (*ownerServices, error) {
		principal, ok := packagingPrincipal(request, governanceService, options)
		if !ok || principal.Username == "" {
			return nil, errors.New("unauthorized")
		}
		actor := principal.Username
		if existing, ok := stores.Load(actor); ok {
			return existing.(*ownerServices), nil
		}
		scopedRunner := runner
		if runner != nil {
			scopedRunner = actorPlanningRunner{runner: runner, actor: actor}
		}
		sessions := planningapp.NewService(scopedRunner).WithAuditor(planningAcceptAudit{repository: repository, actor: actor})
		runs, err := agentstream.NewService(sessions, planningRunAudit{repository: repository, actor: actor})
		if err != nil {
			return nil, err
		}
		actual, _ := stores.LoadOrStore(actor, &ownerServices{sessions: sessions, runs: runs})
		return actual.(*ownerServices), nil
	}
	authorize := func(request *http.Request) error { _, err := operatorClaims(request, options); return err }
	planninghttp.Register(mux, planninghttp.Config{
		ResolveService: func(request *http.Request) (planninghttp.Sessions, error) {
			owner, err := resolve(request)
			if err != nil {
				return nil, err
			}
			return owner.sessions, nil
		},
		Authorize: authorize,
	})
	agentstream.RegisterHTTP(mux, agentstream.HTTPConfig{
		ResolveService: func(request *http.Request) (*agentstream.Service, error) {
			owner, err := resolve(request)
			if err != nil {
				return nil, err
			}
			return owner.runs, nil
		},
		Authorize: authorize,
	})
	mux.Handle("/", legacy)
	return mux
}

func planningPlanner(getenv func(string) string, repository controlplane.Repository) (planningapp.Planner, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	raw := strings.TrimSpace(getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return nil, nil
	}
	argv, err := localcmd.ParseArgv(raw)
	if err != nil {
		return nil, fmt.Errorf("CONDUIT_MODEL_CMD: %w", err)
	}
	timeout := 45 * time.Second
	if text := strings.TrimSpace(getenv("CONDUIT_MODEL_TIMEOUT")); text != "" {
		parsed, err := time.ParseDuration(text)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("CONDUIT_MODEL_TIMEOUT must be a positive duration")
		}
		timeout = parsed
	}
	return planner.Planner{Argv: argv, Timeout: timeout, Audit: planningAudit{repository: repository}}, nil
}

type planningAudit struct {
	repository controlplane.Repository
}

func (audit planningAudit) Record(ctx context.Context, record planner.Record) error {
	if audit.repository == nil {
		return nil
	}
	decision := governance.DecisionDeny
	if record.Outcome == "pass" {
		decision = governance.DecisionAllow
	}
	action := record.Action
	if action == "" {
		action = planner.ActionInspect
	}
	resource := "planning-session/" + record.SessionID
	attributes := map[string]any{
		"adapter": record.Adapter, "promptSha256": record.PromptSHA256,
		"responseSha256": record.ResponseSHA256, "toolCalls": record.ToolCalls,
		"repoUrl": record.RepoURL, "resolvedCommit": record.ResolvedCommit,
	}
	if threadID, runID := planningapp.RunIdentifiers(ctx); threadID != "" && runID != "" {
		attributes["threadId"], attributes["runId"] = threadID, runID
	}
	event := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), planningActor(ctx), action, resource, decision, record.Reason, attributes,
	)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

// planningAcceptAudit commits the acceptance receipt before the service binds it.
type planningAcceptAudit struct {
	repository controlplane.Repository
	actor      string
}

func (audit planningAcceptAudit) Record(ctx context.Context, record planningapp.Audit) error {
	event := governance.NewAuthorizationCheckEvent(record.AcceptedAt, audit.actor, record.Action,
		"planning-session/"+record.SessionID, governance.DecisionAllow, "operator accepted the bound plan",
		map[string]any{"profileName": record.ProfileName, "profileVersion": record.ProfileVersion,
			"profileHash": record.ProfileHash, "repoUrl": record.RepoURL, "ref": record.Ref,
			"intakeMessageSha256": packagingdomain.DigestText([]byte(record.Message)),
			"resolvedCommit":      record.ResolvedCommit, "model": record.Model, "planHash": record.PlanHash})
	_, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

type planningActorKey struct{}

func planningActor(ctx context.Context) string {
	actor, _ := ctx.Value(planningActorKey{}).(string)
	return actor
}

type actorPlanningRunner struct {
	runner planningapp.Planner
	actor  string
}

func (runner actorPlanningRunner) Turn(ctx context.Context, session *planningdomain.Session, message string) (*planningapp.TurnResult, error) {
	return runner.runner.Turn(context.WithValue(ctx, planningActorKey{}, runner.actor), session, message)
}

type planningRunAudit struct {
	repository controlplane.Repository
	actor      string
}

func (audit planningRunAudit) Record(ctx context.Context, record agentstream.AuditRecord) error {
	if audit.repository == nil || audit.actor == "" {
		return agentstream.ErrAuditUnavailable
	}
	decision := governance.DecisionAllow
	if record.Action == "agent.tool_denied" || record.Action == "agent.event.RUN_ERROR" {
		decision = governance.DecisionDeny
	}
	event := governance.NewAuthorizationCheckEvent(time.Now().UTC(), audit.actor, record.Action, "planning-session/"+record.ThreadID, decision, "governed planning run event", map[string]any{"threadId": record.ThreadID, "runId": record.RunID, "payloadSha256": record.PayloadSHA256})
	_, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

type temporalActorRunner struct {
	runner planningapp.Planner
}

func (r temporalActorRunner) TurnForActor(ctx context.Context, actor string, session *planningdomain.Session, message string) (*planningapp.TurnResult, error) {
	if r.runner == nil {
		return nil, planningdomain.ErrModelNotConfigured
	}
	return r.runner.Turn(context.WithValue(ctx, planningActorKey{}, actor), session, message)
}

type temporalAcceptAudit struct {
	repository controlplane.Repository
}

func (a temporalAcceptAudit) RecordAccept(ctx context.Context, actor string, record planningapp.Audit) error {
	return (planningAcceptAudit{repository: a.repository, actor: actor}).Record(ctx, record)
}

