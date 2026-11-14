// Package runtime dials Temporal and runs the packaging worker. The Hub uses
// the client for the read-only status route. The worker process is the only
// place activities run.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/planlifecycle"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/config"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packaginghttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/httpapi"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/pipeline"
)

// maxIndexedRuns is the most workflow executions one index read will load
// before it refuses. Past that, a short total would omit runs the caller is
// allowed to see. The Hub authorizes and paginates the loaded set.
const maxIndexedRuns = 1000

// Dial returns a Temporal client. The caller closes it.
func Dial(cfg config.Config) (client.Client, error) {
	if !cfg.Enabled {
		return nil, errors.New("packaging Temporal is disabled")
	}
	options := client.Options{HostPort: cfg.Address, Namespace: cfg.Namespace}
	tlsConfig, err := cfg.ClientTLS()
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		options.ConnectionOptions.TLS = tlsConfig
	}
	if cfg.APIKey != "" {
		options.Credentials = client.NewAPIKeyStaticCredentials(cfg.APIKey)
	}
	return client.Dial(options)
}

// StatusReader queries packaging-run progress and applies a governed cancel or
// rerun after the Hub has already authorized it. It does not approve or submit.
func StatusReader(c client.Client, taskQueue string) packaginghttp.Controls {
	if taskQueue == "" {
		taskQueue = pipeline.TaskQueue
	}
	return statusReader{client: c, taskQueue: taskQueue}
}

// Unavailable is a status reader used when the Hub was configured with a
// Temporal address but could not dial it. The Hub still serves Plans.
type Unavailable struct{ Err error }

func (u Unavailable) Read(context.Context, string) (domain.RunStatus, error) {
	return domain.RunStatus{}, u.err()
}

func (u Unavailable) List(context.Context) ([]domain.RunStatus, error) {
	return nil, u.err()
}

func (u Unavailable) Events(context.Context, string) ([]domain.StepEvent, error) {
	return nil, u.err()
}

func (u Unavailable) err() error {
	if u.Err == nil {
		return errors.New("packaging Temporal client is unavailable")
	}
	return u.Err
}

func (Unavailable) Configured() bool { return false }

func (u Unavailable) Cancel(context.Context, string) error { return u.err() }

func (u Unavailable) Start(context.Context, domain.RunIdentity) (domain.RunStatus, error) {
	return domain.RunStatus{}, u.err()
}

type statusReader struct {
	client    client.Client
	taskQueue string
}

func (r statusReader) Read(ctx context.Context, runID string) (domain.RunStatus, error) {
	value, err := r.client.QueryWorkflow(ctx, "packaging-"+runID, "", pipeline.QueryStatus)
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			return domain.RunStatus{}, packaginghttp.ErrNotFound
		}
		return domain.RunStatus{}, err
	}
	var status domain.RunStatus
	if err := value.Get(&status); err != nil {
		return domain.RunStatus{}, err
	}
	return status, nil
}

func (r statusReader) List(ctx context.Context) ([]domain.RunStatus, error) {
	runs := make([]domain.RunStatus, 0)
	var token []byte
	for {
		response, err := r.client.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
			PageSize:      200,
			NextPageToken: token,
			Query:         `WorkflowType = "packaging.Run"`,
		})
		if err != nil {
			return nil, err
		}
		for _, execution := range response.GetExecutions() {
			if len(runs) >= maxIndexedRuns {
				return nil, errors.New("packaging run index is larger than 1000 runs")
			}
			workflowID := execution.GetExecution().GetWorkflowId()
			runID := strings.TrimPrefix(workflowID, "packaging-")
			status, err := r.Read(ctx, runID)
			if errors.Is(err, packaginghttp.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if status.RunID == "" {
				status.RunID = runID
			}
			runs = append(runs, status)
		}
		token = response.GetNextPageToken()
		if len(token) == 0 {
			return runs, nil
		}
		if len(runs) >= maxIndexedRuns {
			return nil, errors.New("packaging run index is larger than 1000 runs")
		}
	}
}

func (statusReader) Configured() bool { return true }

func (r statusReader) Cancel(ctx context.Context, runID string) error {
	err := r.client.CancelWorkflow(ctx, "packaging-"+runID, "")
	if err == nil {
		return nil
	}
	var notFound *serviceerror.NotFound
	if errors.As(err, &notFound) {
		return packaginghttp.ErrNotFound
	}
	return err
}

func (r statusReader) Start(ctx context.Context, id domain.RunIdentity) (domain.RunStatus, error) {
	if err := id.Validate(); err != nil {
		return domain.RunStatus{}, err
	}
	if id.ProposalRevision != "" {
		return domain.RunStatus{}, errors.New("a new packaging run does not carry a proposal revision")
	}
	_, err := r.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    id.WorkflowID(),
		TaskQueue:             r.taskQueue,
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, pipeline.WorkflowName, pipeline.Input{Identity: id})
	if err != nil {
		return domain.RunStatus{}, err
	}
	return domain.RunStatus{
		RunID: id.RunID, Phase: domain.PhaseAssess,
		SourceDigest: id.SourceDigest, TargetPlanRef: id.TargetPlanRef,
	}, nil
}

func (r statusReader) Events(ctx context.Context, runID string) ([]domain.StepEvent, error) {
	value, err := r.client.QueryWorkflow(ctx, "packaging-"+runID, "", pipeline.QueryEvents)
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			return nil, packaginghttp.ErrNotFound
		}
		return nil, err
	}
	var events []domain.StepEvent
	if err := value.Get(&events); err != nil {
		return nil, err
	}
	if events == nil {
		events = []domain.StepEvent{}
	}
	return events, nil
}

// RunWorker polls until ctx is cancelled. repository is the delivery kernel's
// store. The worker proposes Plans through it and does not approve them.
func RunWorker(ctx context.Context, cfg config.Config, repository controlplane.Repository) error {
	if !cfg.Enabled {
		return errors.New("PACKAGING_TEMPORAL_ADDRESS is required to run the packaging worker")
	}
	temporalClient, err := Dial(cfg)
	if err != nil {
		return err
	}
	defer temporalClient.Close()
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = ".platform-delivery"
	}
	ledger, err := adapters.Open(dataDir + "/packaging-journal")
	if err != nil {
		return err
	}
	defer ledger.Close()
	work := pipeline.RegisterWorker(temporalClient, cfg.TaskQueue, pipeline.Activities{
		FixtureDir: cfg.FixtureDir,
		Ledger:     ledger,
		Plans:      planlifecycle.New(repository, ledger),
	})
	if err := work.Start(); err != nil {
		return fmt.Errorf("start packaging worker: %w", err)
	}
	<-ctx.Done()
	work.Stop()
	return nil
}

// DialTimeout bounds a Hub's startup dial so a down Temporal frontend cannot
// stall Plan serving.
func DialTimeout(ctx context.Context, cfg config.Config, timeout time.Duration) (client.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		client client.Client
		err    error
	}
	done := make(chan result, 1)
	go func() {
		c, err := Dial(cfg)
		done <- result{c, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-done:
		return got.client, got.err
	}
}
