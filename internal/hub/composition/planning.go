package composition

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.temporal.io/sdk/client"

	appgenapp "github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/agentstream"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	planninghttp "github.com/jonathanagustin/conduit/internal/planning/httpapi"
	planningworkflow "github.com/jonathanagustin/conduit/internal/planning/workflow"
)

// mountPlanning exposes the planning-session contract. When
// CONDUIT_MODEL_CMD is set, turns inspect the intake repository through
// inspect_repo and read_file. When CONDUIT_TEMPORAL_ADDRESS is set, planning
// sessions and SSE agent runs are backed by durable Temporal workflows.
// Compute is refused until Accept.
func mountPlanning(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions) (http.Handler, error) {
	generator, err := appGenerator(os.Getenv)
	if err != nil {
		return nil, err
	}
	appgenService := newAppGenerationService(repository, generator, nil, nil, nil, os.Getenv)
	return mountPlanningWithAppgenService(legacy, repository, options, appgenService, os.Getenv)
}

func mountPlanningWithAppgenService(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions,
	appgenService appgenapp.Service, getenv func(string) string,
) (http.Handler, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	runner, err := planningPlanner(getenv, repository)
	if err != nil {
		return nil, err
	}
	var temporalClient planningworkflow.WorkflowClient
	if addr := strings.TrimSpace(getenv("CONDUIT_TEMPORAL_ADDRESS")); addr != "" {
		host, _, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			return nil, fmt.Errorf("CONDUIT_TEMPORAL_ADDRESS: %w", splitErr)
		}
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("CONDUIT_TEMPORAL_ADDRESS: refusing non-loopback address %q", addr)
		}
		dialed, dialErr := client.NewLazyClient(client.Options{
			HostPort:       addr,
			Namespace:      "default",
			Logger:         planningworkflow.StructuredLogger(slog.Default()),
			MetricsHandler: planningworkflow.NewMetricsRecorder(),
		})
		if dialErr != nil {
			return nil, fmt.Errorf("CONDUIT_TEMPORAL_ADDRESS: %w", dialErr)
		}
		temporalClient = dialed
	}
	return planningHandlerWithOptions(legacy, repository, options, runner, temporalClient, nil, &appgenService, getenv), nil
}

func planningHandler(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, runner planningapp.Planner) http.Handler {
	return planningHandlerWithTemporal(legacy, repository, options, runner, nil)
}

func planningHandlerWithTemporal(legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, runner planningapp.Planner, temporalClient planningworkflow.WorkflowClient) http.Handler {
	return planningHandlerWithOptions(legacy, repository, options, runner, temporalClient, nil, nil, os.Getenv)
}

func planningHandlerWithOptions(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions,
	runner planningapp.Planner, temporalClient planningworkflow.WorkflowClient,
	appgenCompute planningapp.AppgenComputer, appgenService *appgenapp.Service, getenv func(string) string,
) http.Handler {
	var stores sync.Map
	var seq atomic.Uint64
	governanceService := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	type ownerServices struct {
		sessions planninghttp.Sessions
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
		var (
			httpSessions planninghttp.Sessions
			runSessions  agentstream.Sessions
		)
		if temporalClient != nil {
			wfService := planningworkflow.NewClientService(temporalClient, planningworkflow.TaskQueue, actor, &seq)
			httpSessions = wfService
			runSessions = wfService
		} else {
			scopedRunner := runner
			if runner != nil {
				scopedRunner = actorPlanningRunner{runner: runner, actor: actor}
			}
			appgenHook := appgenCompute
			if appgenHook == nil && appgenService != nil {
				appgenHook = appgenPlanningComputer(*appgenService, repository, principal, getenv)
			}
			packagingCompute := packagingPlanningComputer(repository, principal, getenv)
			ownerCompute := func(ctx context.Context, session *planningdomain.Session) error {
				if session == nil || session.Binding == nil {
					return planningdomain.ErrComputeBeforeAccept
				}
				if session.Binding.Profile.Name == planningdomain.ProfileAppgen {
					if appgenHook == nil {
						return planningdomain.ErrComputeNotConfigured
					}
					return appgenHook(ctx, session)
				}
				return packagingCompute(ctx, session)
			}
			inMem := planningapp.NewService(scopedRunner).
				WithAuditor(planningAcceptAudit{repository: repository, actor: actor}).
				WithAppgenCompute(ownerCompute)
			httpSessions = inMem
			runSessions = inMem
		}
		runs, err := agentstream.NewService(runSessions, planningRunAudit{repository: repository, actor: actor})
		if err != nil {
			return nil, err
		}
		actual, _ := stores.LoadOrStore(actor, &ownerServices{sessions: httpSessions, runs: runs})
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
	return planner.Planner{
		Argv:    argv,
		Timeout: timeout,
		Audit:   planningAudit{repository: repository},
		Clone:   hubPlanningClone(getenv("DEVWORKSPACE_GIT_ROOT")),
	}, nil
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
	ctxMap := map[string]any{"profileName": record.ProfileName, "profileVersion": record.ProfileVersion,
		"profileHash": record.ProfileHash, "repoUrl": record.RepoURL, "ref": record.Ref,
		"intakeMessageSha256": packagingdomain.DigestText([]byte(record.Message)),
		"resolvedCommit":      record.ResolvedCommit, "model": record.Model, "planHash": record.PlanHash}
	if record.ProblemStatement != "" {
		ctxMap["problemStatementSha256"] = packagingdomain.DigestText([]byte(record.ProblemStatement))
	}
	if record.AppName != "" {
		ctxMap["appName"] = record.AppName
	}
	event := governance.NewAuthorizationCheckEvent(record.AcceptedAt, audit.actor, record.Action,
		"planning-session/"+record.SessionID, governance.DecisionAllow, "operator accepted the bound plan", ctxMap)
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
	ctxMap := map[string]any{"threadId": record.ThreadID, "runId": record.RunID, "payloadSha256": record.PayloadSHA256}
	if record.Activity != "" && record.ActivitySequence > 0 {
		ctxMap["activity"] = record.Activity
		ctxMap["activitySequence"] = record.ActivitySequence
	}
	event := governance.NewAuthorizationCheckEvent(time.Now().UTC(), audit.actor, record.Action, "planning-session/"+record.ThreadID, decision, "governed planning run event", ctxMap)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, event)
	return err
}
