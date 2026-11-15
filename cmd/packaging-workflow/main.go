package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/planlifecycle"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/devserver"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/pipeline"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	planningworkflow "github.com/jonathanagustin/conduit/internal/planning/workflow"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
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
		Short: "Localhost Temporal packaging and planning proof. Not a production service.",
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
	var planFixture bool
	workerCmd := &cobra.Command{
		Use:   "worker",
		Short: "Run packaging and planning workers against a loopback server",
		RunE: func(*cobra.Command, []string) error {
			acts, repository, err := activitiesWithRepository(stateDir, fixtureDir)
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

			planActs, err := planningActivities(repository, planFixture)
			if err != nil {
				return err
			}
			planWork := planningworkflow.RegisterWorker(c, planningworkflow.TaskQueue, planActs)
			if err := planWork.Start(); err != nil {
				return err
			}
			defer planWork.Stop()

			fmt.Fprintln(os.Stderr, "worker polling", pipeline.TaskQueue, "and", planningworkflow.TaskQueue)
			waitForSignal()
			return nil
		},
	}
	workerCmd.Flags().BoolVar(&planFixture, "plan-fixture", false, "use a deterministic local planning fixture when CONDUIT_MODEL_CMD is unset")
	root.AddCommand(workerCmd)

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
			return encodeJSON(os.Stdout, status)
		},
	})

	addPlanningCommands(root, &address)
	return root
}

func addPlanningCommands(root *cobra.Command, address *string) {
	var sessionID, actor string
	createCmd := &cobra.Command{
		Use:   "plan-create <repo-url> [ref]",
		Short: "Start a durable Temporal planning session workflow",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := "main"
			if len(args) == 2 {
				ref = args[1]
			}
			c, err := dial(*address)
			if err != nil {
				return err
			}
			defer c.Close()
			svc := planningworkflow.NewClientService(c, planningworkflow.TaskQueue, actor, nil)
			session, err := svc.CreateWithID(context.Background(), sessionID, args[0], ref)
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), session)
		},
	}
	createCmd.Flags().StringVar(&sessionID, "session-id", "ps_1", "planning session identifier")
	createCmd.Flags().StringVar(&actor, "actor", "operator", "operator identity owning the session")
	root.AddCommand(createCmd)

	var turnActor string
	turnCmd := &cobra.Command{
		Use:   "plan-turn <session-id> [message]",
		Short: "Send a synchronous Turn update to a Temporal planning session workflow",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			msg := ""
			if len(args) == 2 {
				msg = args[1]
			}
			c, err := dial(*address)
			if err != nil {
				return err
			}
			defer c.Close()
			svc := planningworkflow.NewClientService(c, planningworkflow.TaskQueue, turnActor, nil)
			session, err := svc.Turn(context.Background(), args[0], msg)
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), session)
		},
	}
	turnCmd.Flags().StringVar(&turnActor, "actor", "operator", "operator identity owning the session")
	root.AddCommand(turnCmd)

	var answerActor string
	answerCmd := &cobra.Command{
		Use:   "plan-answer <session-id> <id=value>...",
		Short: "Send a synchronous Answer update to a Temporal planning session workflow",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			answers := make([]planningdomain.Answer, 0, len(args)-1)
			for _, pair := range args[1:] {
				k, v, ok := strings.Cut(pair, "=")
				if !ok || strings.TrimSpace(k) == "" {
					return fmt.Errorf("answer %q must be id=value", pair)
				}
				answers = append(answers, planningdomain.Answer{ID: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
			}
			c, err := dial(*address)
			if err != nil {
				return err
			}
			defer c.Close()
			svc := planningworkflow.NewClientService(c, planningworkflow.TaskQueue, answerActor, nil)
			session, err := svc.Answer(context.Background(), args[0], answers)
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), session)
		},
	}
	answerCmd.Flags().StringVar(&answerActor, "actor", "operator", "operator identity owning the session")
	root.AddCommand(answerCmd)

	var acceptActor string
	acceptCmd := &cobra.Command{
		Use:   "plan-accept <session-id>",
		Short: "Send a synchronous Accept update to bind the plan and commit the audit receipt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial(*address)
			if err != nil {
				return err
			}
			defer c.Close()
			svc := planningworkflow.NewClientService(c, planningworkflow.TaskQueue, acceptActor, nil)
			session, err := svc.Accept(context.Background(), args[0])
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), session)
		},
	}
	acceptCmd.Flags().StringVar(&acceptActor, "actor", "operator", "operator identity owning the session")
	root.AddCommand(acceptCmd)

	var statusActor string
	statusCmd := &cobra.Command{
		Use:   "plan-status <session-id>",
		Short: "Query the current state of a Temporal planning session workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial(*address)
			if err != nil {
				return err
			}
			defer c.Close()
			svc := planningworkflow.NewClientService(c, planningworkflow.TaskQueue, statusActor, nil)
			session, err := svc.Get(context.Background(), args[0])
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), session)
		},
	}
	statusCmd.Flags().StringVar(&statusActor, "actor", "operator", "operator identity owning the session")
	root.AddCommand(statusCmd)
}

func activities(stateDir, fixtureDir string) (pipeline.Activities, error) {
	acts, _, err := activitiesWithRepository(stateDir, fixtureDir)
	return acts, err
}

func activitiesWithRepository(stateDir, fixtureDir string) (pipeline.Activities, controlplane.Repository, error) {
	if fixtureDir == "" {
		fixtureDir = stateDir + "/fixture"
	}
	if err := fixtures.Materialize(fixtureDir); err != nil {
		return pipeline.Activities{}, nil, err
	}
	ledger, err := adapters.Open(stateDir)
	if err != nil {
		return pipeline.Activities{}, nil, err
	}
	repository, err := controlplane.NewFileRepository(stateDir + "/plans")
	if err != nil {
		return pipeline.Activities{}, nil, err
	}
	return pipeline.Activities{
		FixtureDir: fixtureDir, Ledger: ledger, Plans: planlifecycle.New(repository, ledger),
	}, repository, nil
}

func planningActivities(repository controlplane.Repository, useFixture bool) (planningworkflow.Activities, error) {
	var runner planningapp.Planner
	if raw := strings.TrimSpace(os.Getenv("CONDUIT_MODEL_CMD")); raw != "" {
		argv, err := localcmd.ParseArgv(raw)
		if err != nil {
			return planningworkflow.Activities{}, fmt.Errorf("CONDUIT_MODEL_CMD: %w", err)
		}
		runner = planner.Planner{Argv: argv, Timeout: 45 * time.Second}
	} else if useFixture {
		runner = fixturePlanningRunner{}
	}
	return planningworkflow.Activities{
		Planner: runner,
		Auditor: fileAcceptAuditor{repository: repository},
	}, nil
}

type fixturePlanningRunner struct{}

func (fixturePlanningRunner) Turn(_ context.Context, session *planningdomain.Session, message string) (*planningapp.TurnResult, error) {
	profile := planningdomain.ProfileRef{
		Name:    planningdomain.ProfileOnboard,
		Version: packagingskill.Version,
		Hash:    packagingskill.SHA256(),
	}
	if session.Inspection == nil {
		return &planningapp.TurnResult{
			Messages: []planningdomain.Message{{
				Role: planningdomain.RoleModel,
				Text: "Inspected repository: Node service with package.json and README.md. I have 1 question(s) before I can write the plan.",
			}},
			Inspection: &planningdomain.Inspection{
				Language:       "JavaScript",
				PackageManager: "npm",
				Kind:           "service",
				StartCommand:   "node server.js",
				Ports:          []int{8080},
				HasDockerfile:  false,
				Tree:           []string{"README.md", "package.json"},
				ToolCalls:      []string{"inspect_repo", "read_file"},
			},
			NeedsInput: []planningdomain.Question{{
				ID:       "node_version",
				Name:     "node_version",
				Label:    "Which Node version?",
				Type:     planningdomain.QuestionSelect,
				Required: true,
				Options:  []string{"18", "20"},
			}},
			ResolvedCommit: "3a317b550123456789abcdef0123456789abcdef",
			Model:          planningdomain.DefaultModelLabel,
			Profile:        profile,
		}, nil
	}
	summary := "A small Node site."
	if strings.TrimSpace(message) != "" {
		summary = "A small Node site (" + strings.TrimSpace(message) + ")."
	}
	planText := fmt.Sprintf(`{"summary":%q,"baseImage":"node:20-slim","buildSteps":["npm ci","npm run build"],"ports":[3000,8080],"networkAllow":["registry.npmjs.org:443"],"sso":{"required":true,"notes":"operator login"},"risks":["no lockfile"]}`, summary)
	plan := planningdomain.Plan{Text: planText}
	plan.SHA256 = planningdomain.PlanHash(plan)
	return &planningapp.TurnResult{
		Messages: []planningdomain.Message{{
			Role: planningdomain.RoleModel,
			Text: "Completed packaging plan review.",
		}},
		Plan:    &plan,
		Model:   planningdomain.DefaultModelLabel,
		Profile: profile,
	}, nil
}

type fileAcceptAuditor struct {
	repository controlplane.Repository
}

func (a fileAcceptAuditor) RecordAccept(ctx context.Context, actor string, record planningapp.Audit) error {
	if a.repository == nil {
		return nil
	}
	if strings.TrimSpace(actor) == "" {
		actor = "operator"
	}
	event := governance.NewAuthorizationCheckEvent(
		record.AcceptedAt,
		actor,
		record.Action,
		"planning-session/"+record.SessionID,
		governance.DecisionAllow,
		"operator accepted the bound plan",
		map[string]any{
			"profileName":         record.ProfileName,
			"profileVersion":      record.ProfileVersion,
			"profileHash":         record.ProfileHash,
			"repoUrl":             record.RepoURL,
			"ref":                 record.Ref,
			"intakeMessageSha256": domain.DigestText([]byte(record.Message)),
			"resolvedCommit":      record.ResolvedCommit,
			"model":               record.Model,
			"planHash":            record.PlanHash,
		},
	)
	_, err := a.repository.AppendAuthorizationAudit(ctx, event)
	return err
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
	return client.Dial(client.Options{
		HostPort:       address,
		Namespace:      "default",
		Logger:         planningworkflow.StructuredLogger(slog.Default()),
		MetricsHandler: planningworkflow.NewMetricsRecorder(),
	})
}

func encodeJSON(out io.Writer, v any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
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
