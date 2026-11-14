package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/planlifecycle"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/devserver"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/pipeline"
)

// This binary is the localhost demonstration. It is not a Hub service, it
// does not publish artifacts, and it refuses anything that is not loopback.
func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRoot() *cobra.Command {
	var address, cliPath, dbPath, stateDir, fixtureDir string
	root := &cobra.Command{
		Use:   "packaging-workflow",
		Short: "Localhost Temporal packaging proof. Not a production service.",
		PersistentPreRunE: func(*cobra.Command, []string) error {
			namespace := os.Getenv("TEMPORAL_NAMESPACE")
			if os.Getenv("TEMPORAL_API_KEY") != "" || (namespace != "" && namespace != "default") {
				return fmt.Errorf("refusing Temporal Cloud credentials")
			}
			return nil
		},
	}
	root.PersistentFlags().StringVar(&address, "address", "127.0.0.1:7233", "loopback Temporal frontend")
	root.PersistentFlags().StringVar(&cliPath, "cli", os.Getenv("CONDUIT_TEMPORAL_CLI"), "pinned temporal CLI, used by start-server")
	root.PersistentFlags().StringVar(&dbPath, "db", "temporal.db", "sqlite file for start-server")
	root.PersistentFlags().StringVar(&stateDir, "state-dir", "packaging-state", "receipt, approval, and handoff journal directory")
	root.PersistentFlags().StringVar(&fixtureDir, "fixture-dir", "", "workspace for the pinned fixture; default is a directory under --state-dir")

	root.AddCommand(&cobra.Command{
		Use:   "start-server",
		Short: "Run the development server on loopback with a persistent sqlite file",
		RunE: func(*cobra.Command, []string) error {
			running, err := devserver.Start(context.Background(), devserver.Options{CLIPath: cliPath, DBPath: dbPath, Stdout: os.Stdout, Stderr: os.Stderr})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "frontend %s ui http://%s\n", running.Frontend, running.UI)
			waitForSignal()
			return running.Server.Stop()
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "worker",
		Short: "Run one packaging worker against a loopback server",
		RunE: func(*cobra.Command, []string) error {
			acts, err := activities(stateDir, fixtureDir)
			if err != nil {
				return err
			}
			c, err := dial(address)
			if err != nil {
				return err
			}
			defer c.Close()
			work := pipeline.RegisterWorker(c, pipeline.TaskQueue, acts)
			if err := work.Start(); err != nil {
				return err
			}
			defer work.Stop()
			fmt.Fprintln(os.Stderr, "worker polling", pipeline.TaskQueue)
			waitForSignal()
			return nil
		},
	})
	var sourceFile string
	runCommand := &cobra.Command{
		Use:   "run [run-id]",
		Short: "Start one packaging run and return without approving it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runID := "sample-app-1"
			if len(args) == 1 {
				runID = args[0]
			}
			var id domain.RunIdentity
			var err error
			if sourceFile == "" {
				id, err = fixtures.Identity(runID)
			} else {
				body, readErr := os.ReadFile(sourceFile)
				if readErr != nil {
					return readErr
				}
				id, err = fixtures.IdentityFromSource(runID, body)
			}
			if err != nil {
				return err
			}
			c, err := dial(address)
			if err != nil {
				return err
			}
			defer c.Close()
			_, err = c.ExecuteWorkflow(context.Background(), client.StartWorkflowOptions{
				ID: id.WorkflowID(), TaskQueue: pipeline.TaskQueue,
			}, pipeline.WorkflowName, pipeline.Input{Identity: id, ApprovalWait: 15 * time.Minute})
			return err
		},
	}
	runCommand.Flags().StringVar(&sourceFile, "source-file", "", "source whose digest binds this run; the worker reads sources/<run-id>.txt")
	root.AddCommand(runCommand)
	root.AddCommand(&cobra.Command{
		Use:   "approve [run-id]",
		Short: "Record a stand-in Conduit approval and then signal the waiting run",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runID := "sample-app-1"
			if len(args) == 1 {
				runID = args[0]
			}
			return approve(address, stateDir, fixtureDir, runID)
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "status [run-id]",
		Short: "Print the packaging-run read model. This is not Plan status and not an approval.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runID := "sample-app-1"
			if len(args) == 1 {
				runID = args[0]
			}
			c, err := dial(address)
			if err != nil {
				return err
			}
			defer c.Close()
			id, err := fixtures.Identity(runID)
			if err != nil {
				return err
			}
			value, err := c.QueryWorkflow(context.Background(), id.WorkflowID(), "", pipeline.QueryStatus)
			if err != nil {
				return err
			}
			var status domain.RunStatus
			if err := value.Get(&status); err != nil {
				return err
			}
			status.Tier = "CONDUIT_NATIVE"
			status.Authority = "packaging-run"
			status.PlanAuthority = "conduit-plan"
			status.ApprovalAuthority = "conduit-governance"
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(status)
		},
	})
	return root
}

func activities(stateDir, fixtureDir string) (pipeline.Activities, error) {
	if fixtureDir == "" {
		fixtureDir = stateDir + "/fixture"
	}
	if err := fixtures.Materialize(fixtureDir); err != nil {
		return pipeline.Activities{}, err
	}
	ledger, err := adapters.Open(stateDir)
	if err != nil {
		return pipeline.Activities{}, err
	}
	repository, err := controlplane.NewFileRepository(stateDir + "/plans")
	if err != nil {
		return pipeline.Activities{}, err
	}
	return pipeline.Activities{
		FixtureDir: fixtureDir, Ledger: ledger, Plans: planlifecycle.New(repository, ledger),
	}, nil
}

func approve(address, stateDir, fixtureDir, runID string) error {
	acts, err := activities(stateDir, fixtureDir)
	if err != nil {
		return err
	}
	id, err := fixtures.Identity(runID)
	if err != nil {
		return err
	}
	c, err := dial(address)
	if err != nil {
		return err
	}
	defer c.Close()
	value, err := c.QueryWorkflow(context.Background(), id.WorkflowID(), "", pipeline.QueryStatus)
	if err != nil {
		return err
	}
	var status domain.RunStatus
	if err := value.Get(&status); err != nil {
		return err
	}
	if status.Phase != domain.PhaseReview || status.ArtifactDigest == "" || status.ProposalRevision == "" {
		return fmt.Errorf("run is %s, not a review of a bound proposal", status.Phase)
	}
	record := domain.ApprovalRecord{
		ApprovalID: "appr-" + runID, RunID: id.RunID, ProposalRevision: status.ProposalRevision,
		ArtifactDigest: status.ArtifactDigest, TargetPlanRef: id.TargetPlanRef, Decision: domain.DecisionApproved,
	}
	// The record is written before the signal. The signal does not grant the approval.
	if err := acts.Ledger.PutApproval(record); err != nil {
		return err
	}
	return c.SignalWorkflow(context.Background(), id.WorkflowID(), "", pipeline.SignalReview, domain.ReviewMessage{
		ApprovalID: record.ApprovalID, RunID: record.RunID, ProposalRevision: record.ProposalRevision,
		ArtifactDigest: record.ArtifactDigest, TargetPlanRef: record.TargetPlanRef,
	})
}

func dial(address string) (client.Client, error) {
	if err := requireLoopback(address); err != nil {
		return nil, err
	}
	return client.Dial(client.Options{HostPort: address, Namespace: "default"})
}

func waitForSignal() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
}

func requireLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("refusing non-loopback Temporal address %q", address)
	}
	return nil
}
