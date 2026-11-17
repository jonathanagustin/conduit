package composition

import (
	"context"
	"errors"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/modelconfig"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.temporal.io/sdk/client"

	appgenapp "github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/agentstream"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	planninghttp "github.com/jonathanagustin/conduit/internal/planning/httpapi"
	planningworkflow "github.com/jonathanagustin/conduit/internal/planning/workflow"
)

// mountPlanning exposes the planning-session contract. When
// CONDUIT_MODEL_CMD or CONDUIT_MODEL_BASE_URL is set, turns inspect the intake repository through
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
	if err := validatePlanningReplicas(getenv); err != nil {
		return nil, err
	}
	runner, err := planningPlanner(getenv, repository)
	if err != nil {
		return nil, err
	}
	var temporalClient planningworkflow.WorkflowClient
	if strings.TrimSpace(getenv("CONDUIT_TEMPORAL_ADDRESS")) != "" {
		clientOptions, optErr := planningClientOptions(getenv)
		if optErr != nil {
			return nil, optErr
		}
		dialed, dialErr := client.NewLazyClient(clientOptions)
		if dialErr != nil {
			return nil, fmt.Errorf("CONDUIT_TEMPORAL_ADDRESS: %w", dialErr)
		}
		temporalClient = dialed
	}
	return planningHandlerWithOptions(legacy, repository, options, runner, temporalClient, nil, &appgenService, getenv), nil
}

// ValidatePlanningReplicas rejects a multi-replica Hub whose planning sessions
// live only in the process. One replica may stay process-local. A shared
// workflow frontend is the durable backend the production chart configures.
func ValidatePlanningReplicas(getenv func(string) string) error {
	return validatePlanningReplicas(getenv)
}

func validatePlanningReplicas(getenv func(string) string) error {
	if getenv == nil {
		return errors.New("environment reader is required")
	}
	raw := strings.TrimSpace(getenv("HUB_REPLICA_COUNT"))
	if raw == "" {
		return nil
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 1 {
		return fmt.Errorf("HUB_REPLICA_COUNT must be a positive integer, got %q", raw)
	}
	if count == 1 {
		return nil
	}
	if strings.TrimSpace(getenv("CONDUIT_TEMPORAL_ADDRESS")) == "" {
		return fmt.Errorf("HUB_REPLICA_COUNT=%d uses a process-local planning store; set CONDUIT_TEMPORAL_ADDRESS to a shared workflow frontend or run one replica", count)
	}
	if !planningTemporalShared(getenv) {
		return fmt.Errorf("HUB_REPLICA_COUNT=%d requires CONDUIT_TEMPORAL_SHARED=true so replicas share one workflow frontend", count)
	}
	return nil
}

func planningTemporalShared(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv("CONDUIT_TEMPORAL_SHARED")), "true")
}

func planningTaskQueue(getenv func(string) string) string {
	queue := strings.TrimSpace(getenv("CONDUIT_TEMPORAL_TASK_QUEUE"))
	if queue == "" {
		return planningworkflow.TaskQueue
	}
	return queue
}

func planningClientOptions(getenv func(string) string) (client.Options, error) {
	address := strings.TrimSpace(getenv("CONDUIT_TEMPORAL_ADDRESS"))
	if address == "" {
		return client.Options{}, errors.New("CONDUIT_TEMPORAL_ADDRESS is required")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return client.Options{}, fmt.Errorf("CONDUIT_TEMPORAL_ADDRESS: %w", err)
	}
	shared := planningTemporalShared(getenv)
	if ip := net.ParseIP(host); ip != nil {
		if !ip.IsLoopback() && !shared {
			return client.Options{}, fmt.Errorf("CONDUIT_TEMPORAL_ADDRESS: refusing non-loopback address %q", address)
		}
	} else if !shared {
		return client.Options{}, fmt.Errorf("CONDUIT_TEMPORAL_ADDRESS: refusing non-loopback address %q", address)
	}
	namespace := strings.TrimSpace(getenv("CONDUIT_TEMPORAL_NAMESPACE"))
	if namespace == "" {
		namespace = "default"
	}
	return client.Options{
		HostPort:       address,
		Namespace:      namespace,
		Logger:         planningworkflow.StructuredLogger(slog.Default()),
		MetricsHandler: planningworkflow.NewMetricsRecorder(),
	}, nil
}

// StartPlanningWorker polls the planning task queue when a workflow frontend
// is configured. The returned function stops the worker. An empty address
// leaves planning on the process-local store and returns a no-op stop.
func StartPlanningWorker(ctx context.Context, getenv func(string) string, repository controlplane.Repository) (func(), error) {
	noop := func() {}
	if getenv == nil {
		return nil, errors.New("environment reader is required")
	}
	if strings.TrimSpace(getenv("CONDUIT_TEMPORAL_ADDRESS")) == "" {
		return noop, nil
	}
	if err := validatePlanningReplicas(getenv); err != nil {
		return nil, err
	}
	options, err := planningClientOptions(getenv)
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	temporalClient, err := client.DialContext(dialCtx, options)
	if err != nil {
		return nil, fmt.Errorf("CONDUIT_TEMPORAL_ADDRESS: %w", err)
	}
	runner, err := planningPlanner(getenv, repository)
	if err != nil {
		temporalClient.Close()
		return nil, err
	}
	work := planningworkflow.RegisterWorker(temporalClient, planningTaskQueue(getenv), planningworkflow.Activities{
		Planner: runner,
		Auditor: workflowAcceptAuditor{repository: repository},
	})
	if err := work.Start(); err != nil {
		temporalClient.Close()
		return nil, fmt.Errorf("start planning workflow worker: %w", err)
	}
	return func() {
		work.Stop()
		temporalClient.Close()
	}, nil
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
	governanceService := newGovernanceService(repository, options)
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
		ownerCompute := sessionCompute(appgenCompute, appgenService, repository, principal, getenv, options)
		if temporalClient != nil {
			wfService := planningworkflow.NewClientService(temporalClient, planningTaskQueue(getenv), actor, &seq).
				WithCompute(ownerCompute)
			httpSessions = wfService
			runSessions = wfService
		} else {
			scopedRunner := runner
			if runner != nil {
				scopedRunner = actorPlanningRunner{runner: runner, actor: actor}
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

func sessionCompute(
	appgenCompute planningapp.AppgenComputer, appgenService *appgenapp.Service,
	repository controlplane.Repository, principal controlplane.Principal, getenv func(string) string,
	handlerOptions ...controlplane.HandlerOptions,
) planningapp.AppgenComputer {
	hook := appgenCompute
	if hook == nil && appgenService != nil {
		hook = appgenPlanningComputer(*appgenService, repository, principal, getenv, handlerOptions...)
	}
	packaging := packagingPlanningComputer(repository, principal, getenv, handlerOptions...)
	computer := func(ctx context.Context, session *planningdomain.Session) error {
		if session == nil || session.Binding == nil {
			return planningdomain.ErrComputeBeforeAccept
		}
		if session.Binding.Profile.Name == planningdomain.ProfileAppgen {
			if hook == nil {
				return planningdomain.ErrComputeNotConfigured
			}
			return hook(ctx, session)
		}
		return packaging(ctx, session)
	}
	if store, ok := repository.(computeExecutionStore); ok {
		return guardCompute(store, computer)
	}
	return computer
}

func planningPlanner(getenv func(string) string, repository controlplane.Repository) (planningapp.Planner, error) {
	getenv = modelconfig.ForCapability(getenv, modelconfig.Planning)
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	// The source-host policy applies to every transport, so a value that does
	// not parse stops startup before a model is chosen.
	clone, err := hubPlanningClone(getenv)
	if err != nil {
		return nil, err
	}
	// One transport drives planning turns. Both set is a startup error and
	// neither leaves planning unconfigured; the rule is modelHTTPClient's.
	if client, err := modelHTTPClient(getenv); err != nil {
		return nil, err
	} else if client != nil {
		return planner.Planner{
			Model: planner.HTTPModel{Client: client},
			Audit: planningAudit{repository: repository},
			Clone: clone,
		}, nil
	}
	return nil, nil
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
		// Both ids come from configuration and provider metadata. An empty
		// value means that source had none; it is recorded, not guessed.
		"configuredModel": record.ConfiguredModel, "providerModel": record.ProviderModel,
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
	return recordPlanningAccept(ctx, audit.repository, audit.actor, record)
}

type workflowAcceptAuditor struct {
	repository controlplane.Repository
}

func (audit workflowAcceptAuditor) RecordAccept(ctx context.Context, actor string, record planningapp.Audit) error {
	if strings.TrimSpace(actor) == "" {
		actor = "operator"
	}
	return recordPlanningAccept(ctx, audit.repository, actor, record)
}

func recordPlanningAccept(ctx context.Context, repository controlplane.Repository, actor string, record planningapp.Audit) error {
	ctxMap := map[string]any{"profileName": record.ProfileName, "profileVersion": record.ProfileVersion,
		"profileHash": record.ProfileHash, "repoUrl": record.RepoURL, "ref": record.Ref,
		"intakeMessageSha256": packagingdomain.DigestText([]byte(record.Message)),
		"resolvedCommit":      record.ResolvedCommit, "model": record.Model, "planHash": record.PlanHash,
		"configuredModel": record.ConfiguredModel, "providerModel": record.ProviderModel}
	if record.ProblemStatement != "" {
		ctxMap["problemStatementSha256"] = packagingdomain.DigestText([]byte(record.ProblemStatement))
	}
	if record.AppName != "" {
		ctxMap["appName"] = record.AppName
	}
	event := governance.NewAuthorizationCheckEvent(record.AcceptedAt, actor, record.Action,
		"planning-session/"+record.SessionID, governance.DecisionAllow, "operator accepted the bound plan", ctxMap)
	_, err := repository.AppendAuthorizationAudit(ctx, event)
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
