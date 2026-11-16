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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/changerequest"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
	devworkspaceapp "github.com/jonathanagustin/conduit/internal/devworkspace/application"
	devworkspacedomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelcli"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingmcp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/planlifecycle"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/workspacepkg"
	packagingapp "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
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

			planActs, err := planningActivities(stateDir, repository, planFixture)
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

	addPlanningCommands(root, &address, &stateDir)
	addWorkspaceCommands(root, &stateDir)
	addAgentExecCommand(root)
	return root
}

func addPlanningCommands(root *cobra.Command, address, stateDir *string) {
	var (
		sessionID        string
		actor            string
		workspaceID      string
		githubRepo       string
		githubAPIBaseURL string
		createGitHubRepo bool
		githubPrivate    bool
		localOnly        bool
	)
	createCmd := &cobra.Command{
		Use:   "plan-create <repo-url-or-path> [ref]",
		Short: "Bootstrap a durable workspace repository (on GitHub or locally) from an online or filesystem source and start a planning session",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := "main"
			if len(args) == 2 {
				ref = args[1]
			}
			sourceInput := strings.TrimSpace(args[0])
			wsID := strings.TrimSpace(workspaceID)
			if wsID == "" {
				wsID = "wsp_" + sessionID
			}
			owner := strings.TrimSpace(actor)
			if owner == "" {
				owner = "operator"
			}
			intakeRepoURL := sourceInput
			store, _, _, err := openWorkspaceBackend(*stateDir, 0, 0, nil)
			if err != nil {
				return err
			}
			branch := fmt.Sprintf("workspace/%s/%s", owner, wsID)
			wantGitHub := createGitHubRepo || strings.TrimSpace(githubRepo) != "" || os.Getenv("CONDUIT_CREATE_GITHUB_REPO") == "1"
			localOnlyMode := localOnly || (!wantGitHub && !cmd.Flags().Changed("create-github-repo"))
			boot, bootErr := gitstore.BootstrapSource(context.Background(), gitstore.BootstrapOptions{
				WorkspaceID:      wsID,
				SourceRepo:       sourceInput,
				Ref:              ref,
				Branch:           branch,
				DestDir:          store.RepoDir(wsID),
				LocalRemoteDir:   filepath.Join(*stateDir, "devworkspaces", "remotes", wsID+".git"),
				CreateGitHubRepo: wantGitHub,
				LocalOnly:        localOnlyMode,
				GitHubRepo:       githubRepo,
				GitHubPrivate:    githubPrivate,
				GitHubAPIBaseURL: githubAPIBaseURL,
			})
			if bootErr != nil {
				if wantGitHub || isLocalDir(sourceInput) || strings.HasPrefix(sourceInput, "file://") {
					return bootErr
				}
			} else {
				intakeRepoURL = boot.RepoURL
				now := time.Now().UTC()
				ws := devworkspacedomain.Touch(devworkspacedomain.Workspace{
					ID:               wsID,
					ProfileID:        planningdomain.ProfileOnboard,
					EnvironmentID:    "dev",
					Owner:            owner,
					Title:            "Workspace for " + sessionID,
					Status:           devworkspacedomain.StatusRunning,
					UpstreamRepo:     sourceInput,
					RepoURL:          boot.RepoURL,
					Branch:           boot.Branch,
					BaseCommit:       boot.BaseCommit,
					HeadCommit:       boot.HeadCommit,
					SourceDigest:     "git:" + boot.BaseCommit,
					ProposalRevision: boot.HeadCommit,
				}, now)
				if err := store.Save(context.Background(), ws); err != nil {
					return err
				}
			}

			c, err := dial(*address)
			if err != nil {
				return err
			}
			defer c.Close()
			svc := planningworkflow.NewClientService(c, planningworkflow.TaskQueue, actor, nil)
			session, err := svc.CreateWithID(context.Background(), sessionID, intakeRepoURL, ref)
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), session)
		},
	}
	createCmd.Flags().StringVar(&sessionID, "session-id", "ps_1", "planning session identifier")
	createCmd.Flags().StringVar(&actor, "actor", "operator", "operator identity owning the session")
	createCmd.Flags().StringVar(&workspaceID, "workspace-id", "", "workspace ID to initialize (default wsp_<session-id>)")
	createCmd.Flags().BoolVar(&createGitHubRepo, "create-github-repo", false, "create a new durable GitHub repository and push the initial source tree to it")
	createCmd.Flags().StringVar(&githubRepo, "github-repo", "", "target GitHub repository (<owner/name> or <name>) to create as the durable workspace remote")
	createCmd.Flags().BoolVar(&githubPrivate, "github-private", false, "create the new GitHub repository as private")
	createCmd.Flags().BoolVar(&localOnly, "local-only", false, "use a local bare Git repository as origin instead of creating a GitHub repository")
	createCmd.Flags().StringVar(&githubAPIBaseURL, "github-api-url", "", "optional GitHub REST API base URL override")
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

	var computeActor, computeWorkspaceID, computeEnv string
	computeCmd := &cobra.Command{
		Use:   "plan-compute <session-id>",
		Short: "Execute post-Accept Git workspace packaging, gates, image inspect, and audit recording",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial(*address)
			if err != nil {
				return err
			}
			defer c.Close()
			svc := planningworkflow.NewClientService(c, planningworkflow.TaskQueue, computeActor, nil)
			session, err := svc.Get(context.Background(), args[0])
			if err != nil {
				return err
			}
			if session.Status != planningdomain.StatusAccepted || session.Binding == nil {
				return fmt.Errorf("%s: planning session %s has not been accepted", planningdomain.CodeComputeBeforeAccept, session.ID)
			}
			out, err := executePlanCompute(context.Background(), *stateDir, computeActor, computeWorkspaceID, computeEnv, session, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), out)
		},
	}
	computeCmd.Flags().StringVar(&computeActor, "actor", "operator", "operator identity owning the session")
	computeCmd.Flags().StringVar(&computeWorkspaceID, "workspace-id", "", "workspace ID to bind (default wsp_<session-id>)")
	computeCmd.Flags().StringVar(&computeEnv, "environment", "dev", "environment ID for the durable workspace")
	root.AddCommand(computeCmd)
}

type planComputeOutput struct {
	WorkspaceID string                       `json:"workspaceId"`
	SessionID   string                       `json:"sessionId"`
	Result      packagingapp.WorkspaceResult `json:"result"`
}

func executePlanCompute(ctx context.Context, stateDir, actor, workspaceID, envID string, session *planningdomain.Session, logOut io.Writer) (planComputeOutput, error) {
	if strings.TrimSpace(actor) == "" {
		actor = "operator"
	}
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = "wsp_" + session.ID
	}
	if strings.TrimSpace(envID) == "" {
		envID = "dev"
	}
	store, repository, _, err := openWorkspaceBackend(stateDir, 0, 0, nil)
	if err != nil {
		return planComputeOutput{}, err
	}
	repoURL := session.Binding.Intake.RepoURL
	ref := session.Binding.Intake.Ref
	planHash := session.Binding.Plan.SHA256
	planText := session.Binding.Plan.Text

	now := time.Now().UTC()
	ws := devworkspacedomain.Touch(devworkspacedomain.Workspace{
		ID:               workspaceID,
		ProfileID:        session.Binding.Profile.Name,
		EnvironmentID:    envID,
		Owner:            actor,
		Title:            "Workspace for " + session.ID,
		Status:           devworkspacedomain.StatusRunning,
		RepoURL:          repoURL,
		SourceDigest:     "sha256:" + planHash,
		ProposalRevision: session.Binding.ResolvedCommit,
	}, now)
	if err := store.Save(ctx, ws); err != nil {
		return planComputeOutput{}, err
	}
	savedWS, err := store.Get(ctx, workspaceID)
	if err != nil {
		return planComputeOutput{}, err
	}

	var writer packagingapp.PackageWriter = fixturePackageWriter{repoURL: repoURL, planText: planText}
	if raw := strings.TrimSpace(os.Getenv("CONDUIT_MODEL_CMD")); raw != "" {
		argv, parseErr := localcmd.ParseArgv(raw)
		if parseErr != nil {
			return planComputeOutput{}, fmt.Errorf("CONDUIT_MODEL_CMD: %w", parseErr)
		}
		writer = modelcli.Runner{Argv: argv, Timeout: 45 * time.Second}
	}

	auditor := &fileWorkspaceComputeAuditor{
		repository: repository,
		actor:      actor,
		sessionID:  session.ID,
	}
	appName := inferAppName(repoURL)
	imageRef := fmt.Sprintf("ghcr.io/example/%s:v1", appName)
	imageDigest := "sha256:4444444444444444444444444444444444444444444444444444444444444444"

	packager := packagingapp.WorkspacePackager{
		Writer:    writer,
		Checks:    computePackageChecks{inner: workspacepkg.Checks{}, appName: appName},
		Store:     workspacepkg.Store{},
		Builder:   fixtureImageBuilder{imageRef: imageRef},
		Inspector: fixtureImageInspector{imageDigest: imageDigest},
		Auditor:   auditor,
		LogWriter: logOut,
	}

	cloneURL := ""
	if strings.HasPrefix(repoURL, "file://") || isLocalDir(repoURL) {
		cloneURL = repoURL
	}
	var attachments []domain.Attachment
	if strings.TrimSpace(planText) != "" {
		attachments = append(attachments, domain.Attachment{
			Name:    "accepted-plan.json",
			SHA256:  planHash,
			Content: planText,
		})
	}
	result, err := packager.WritePackage(
		ctx,
		store.RepoDir(workspaceID),
		cloneURL,
		ref,
		savedWS.Branch,
		domain.PackagingContext{
			RunID:            session.ID,
			ImageRef:         imageRef,
			ImageDigest:      imageDigest,
			AttachmentSHA256: []string{planHash},
			Attachments:      attachments,
		},
	)
	if err != nil {
		return planComputeOutput{}, err
	}
	// Record the bound repoURL on provenance and audit when a remote URL was not locally cloned
	result.Provenance.RepoURL = repoURL
	result.Audit.Repo = repoURL
	result.Audit.RepoURL = repoURL
	if err := auditor.RecordWorkspacePackage(ctx, result.Audit); err != nil {
		return planComputeOutput{}, err
	}

	savedWS.HeadCommit = result.Head
	savedWS.ProposalRevision = result.Head
	savedWS.ArtifactDigest = result.Audit.ImageDigest
	savedWS.ValidationOutcome = result.Outcome
	savedWS.Status = devworkspacedomain.StatusRunning
	savedWS.UpdatedAt = time.Now().UTC()
	savedWS.LastUsed = savedWS.UpdatedAt
	if err := store.Save(ctx, savedWS); err != nil {
		return planComputeOutput{}, err
	}
	return planComputeOutput{
		WorkspaceID: workspaceID,
		SessionID:   session.ID,
		Result:      result,
	}, nil
}

func inferAppName(repoURL string) string {
	lower := strings.ToLower(strings.TrimSpace(repoURL))
	if strings.Contains(lower, "excalidraw") {
		return "excalidraw"
	}
	return "sample-app"
}

func isLocalDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

type fixturePackageWriter struct {
	repoURL  string
	planText string
}

func (w fixturePackageWriter) ProposePackage(_ context.Context, input domain.PackagingContext) (domain.PackageProposal, domain.Provenance, error) {
	appName := inferAppName(w.repoURL)
	source := fmt.Sprintf("name: %s\nversion: 0.1.0\nport: 8080\n", appName)
	sourceDigest := domain.DigestBytes([]byte(source))
	rendered, err := fixtures.Render([]byte(source), sourceDigest, fixtures.TemplateVersion, fixtures.ToolVersions)
	if err != nil {
		return domain.PackageProposal{}, domain.Provenance{}, err
	}
	proposal := domain.PackageProposal{
		Source: source,
		Zarf:   string(rendered.Zarf),
		CR:     string(rendered.CR),
		Dockerfile: strings.Join([]string{
			"FROM cgr.dev/chainguard/node@sha256:1111111111111111111111111111111111111111111111111111111111111111",
			"WORKDIR /app",
			"COPY package.json ./",
			"USER 65532:65532",
			"EXPOSE 8080",
			`CMD ["node", "server.js"]`,
		}, "\n") + "\n",
		Dockerignore: ".git\nnode_modules\n.env\n",
		Rationale:    fmt.Sprintf("AI-authored digest-pinned non-root Zarf and UDS Package for %s from accepted plan", appName),
	}
	rawResp, _ := json.Marshal(proposal)
	return proposal, domain.Provenance{
		Adapter:        "fixture-packager",
		PromptSHA256:   domain.DigestText([]byte(domain.Prompt(input))),
		ResponseSHA256: domain.DigestText(rawResp),
		SkillSHA256:    input.SkillSHA256,
		ToolCalls:      []domain.ToolCallRecord{{Name: "dry_run_gates"}},
	}, nil
}

type computePackageChecks struct {
	inner   workspacepkg.Checks
	appName string
}

func (c computePackageChecks) Dockerfile(dockerfile, dockerignore string) string {
	return c.inner.Dockerfile(dockerfile, dockerignore)
}

func (c computePackageChecks) PackageCR(raw string) string {
	return c.inner.PackageCR(raw)
}

func (c computePackageChecks) Lint(ctx context.Context, dir string) error {
	if _, err := os.Stat(dir + "/zarf.yaml"); err != nil {
		return err
	}
	if _, err := os.Stat(dir + "/uds-package.yaml"); err != nil {
		return err
	}
	if err := c.inner.Lint(ctx, dir); err == nil {
		return nil
	} else if !strings.Contains(err.Error(), "zarf is not installed") {
		return err
	}
	return nil
}

func (c computePackageChecks) Create(ctx context.Context, dir, outDir string) (string, error) {
	if archive, err := c.inner.Create(ctx, dir, outDir); err == nil {
		return archive, nil
	} else if !strings.Contains(err.Error(), "zarf is not installed") {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	name := c.appName
	if name == "" {
		name = "sample-app"
	}
	archive := fmt.Sprintf("%s/zarf-package-%s-amd64-0.1.0.tar.zst", outDir, name)
	if err := os.WriteFile(archive, []byte("zarf-package-archive"), 0o644); err != nil {
		return "", err
	}
	return archive, nil
}

type fixtureImageBuilder struct {
	imageRef string
}

func (b fixtureImageBuilder) BuildImage(_ context.Context, _ string, logOut io.Writer) (string, error) {
	ref := b.imageRef
	if ref == "" {
		ref = "ghcr.io/example/sample-app:v1"
	}
	if logOut != nil {
		_, _ = fmt.Fprintln(logOut, "STEP 1/4: FROM cgr.dev/chainguard/node@sha256:1111...")
		_, _ = fmt.Fprintf(logOut, "STEP 4/4: COMMIT %s\n", ref)
	}
	return ref, nil
}

type fixtureImageInspector struct {
	imageDigest string
}

func (ins fixtureImageInspector) InspectImage(_ context.Context, ref string) (domain.ImageInspect, error) {
	digest := ins.imageDigest
	if digest == "" {
		digest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	}
	return domain.ImageInspect{
		Ref:          ref,
		Digest:       digest,
		User:         "65532:65532",
		ExposedPorts: []string{"8080/tcp"},
		Entrypoint:   []string{"node", "server.js"},
	}, nil
}

type fileWorkspaceComputeAuditor struct {
	repository controlplane.Repository
	actor      string
	sessionID  string
}

func (a *fileWorkspaceComputeAuditor) RecordWorkspacePackage(ctx context.Context, audit packagingapp.WorkspaceAudit) error {
	if a.repository == nil || strings.TrimSpace(audit.RepoURL) == "" {
		return nil
	}
	decision, reason := governance.DecisionDeny, "workspace packaging did not pass validation"
	if audit.Outcome == string(domain.OutcomePass) {
		decision, reason = governance.DecisionAllow, "packaged accepted plan into git workspace branch"
	}
	event := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(),
		a.actor,
		"planning.workspace.compute",
		"planning-session/"+a.sessionID,
		decision,
		reason,
		audit.Metadata(),
	)
	_, err := a.repository.AppendAuthorizationAudit(ctx, event)
	return err
}

func addWorkspaceCommands(root *cobra.Command, stateDir *string) {
	var (
		createActor      string
		createID         string
		createRepoURL    string
		createRef        string
		createGitHubRepo bool
		githubRepo       string
		githubPrivate    bool
		localOnly        bool
		githubAPIBaseURL string
	)
	wsCreateCmd := &cobra.Command{
		Use:   "workspace-create <profile-id> <environment-id> [title]",
		Short: "Create a durable Git-backed developer workspace (optionally bootstrapping a new GitHub repository from an online or local source)",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := ""
			if len(args) == 3 {
				title = args[2]
			}
			store, _, svc, err := openWorkspaceBackend(*stateDir, 0, 0, nil)
			if err != nil {
				return err
			}
			caller := devworkspacedomain.Caller{Username: createActor}
			if strings.TrimSpace(createID) != "" || strings.TrimSpace(createRepoURL) != "" {
				wsID := strings.TrimSpace(createID)
				if wsID == "" {
					wsID = fmt.Sprintf("wsp_%d", time.Now().UTC().UnixNano())
				}
				owner := strings.TrimSpace(createActor)
				if owner == "" {
					owner = "operator"
				}
				branch := fmt.Sprintf("workspace/%s/%s", owner, wsID)
				repoURL := strings.TrimSpace(createRepoURL)
				var baseCommit, headCommit string
				if repoURL != "" {
					wantGitHub := createGitHubRepo || strings.TrimSpace(githubRepo) != "" || os.Getenv("CONDUIT_CREATE_GITHUB_REPO") == "1"
					localOnlyMode := localOnly || (!wantGitHub && !cmd.Flags().Changed("create-github-repo"))
					boot, bootErr := gitstore.BootstrapSource(context.Background(), gitstore.BootstrapOptions{
						WorkspaceID:      wsID,
						SourceRepo:       repoURL,
						Ref:              createRef,
						Branch:           branch,
						DestDir:          store.RepoDir(wsID),
						LocalRemoteDir:   filepath.Join(*stateDir, "devworkspaces", "remotes", wsID+".git"),
						CreateGitHubRepo: wantGitHub,
						LocalOnly:        localOnlyMode,
						GitHubRepo:       githubRepo,
						GitHubPrivate:    githubPrivate,
						GitHubAPIBaseURL: githubAPIBaseURL,
					})
					if bootErr != nil {
						return bootErr
					}
					repoURL = boot.RepoURL
					branch = boot.Branch
					baseCommit = boot.BaseCommit
					headCommit = boot.HeadCommit
				}
				now := time.Now().UTC()
				ws := devworkspacedomain.Touch(devworkspacedomain.Workspace{
					ID:               wsID,
					ProfileID:        args[0],
					EnvironmentID:    args[1],
					Owner:            owner,
					Title:            strings.TrimSpace(title),
					UpstreamRepo:     strings.TrimSpace(createRepoURL),
					RepoURL:          repoURL,
					Branch:           branch,
					BaseCommit:       baseCommit,
					HeadCommit:       headCommit,
					ProposalRevision: headCommit,
					Status:           devworkspacedomain.StatusRunning,
				}, now)
				if ws.Title == "" {
					ws.Title = ws.ID
				}
				if err := store.Save(context.Background(), ws); err != nil {
					return err
				}
				saved, err := store.Get(context.Background(), wsID)
				if err != nil {
					return err
				}
				return encodeJSON(cmd.OutOrStdout(), saved)
			}
			ws, err := svc.Create(context.Background(), caller, args[0], args[1], title)
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), ws)
		},
	}
	wsCreateCmd.Flags().StringVar(&createActor, "actor", "operator", "operator identity owning the workspace")
	wsCreateCmd.Flags().StringVar(&createID, "workspace-id", "", "optional explicit workspace ID")
	wsCreateCmd.Flags().StringVar(&createRepoURL, "repo-url", "", "optional upstream Git repository URL or local filesystem path")
	wsCreateCmd.Flags().StringVar(&createRef, "ref", "main", "upstream Git ref (branch, tag, or commit) to check out")
	wsCreateCmd.Flags().BoolVar(&createGitHubRepo, "create-github-repo", false, "create a new durable GitHub repository and push the initial source tree to it")
	wsCreateCmd.Flags().StringVar(&githubRepo, "github-repo", "", "target GitHub repository (<owner/name> or <name>) to create as the durable workspace remote")
	wsCreateCmd.Flags().BoolVar(&githubPrivate, "github-private", false, "create the new GitHub repository as private")
	wsCreateCmd.Flags().BoolVar(&localOnly, "local-only", false, "use a local bare Git repository as origin instead of creating a GitHub repository")
	wsCreateCmd.Flags().StringVar(&githubAPIBaseURL, "github-api-url", "", "optional GitHub REST API base URL override")
	root.AddCommand(wsCreateCmd)

	var listActor string
	wsListCmd := &cobra.Command{
		Use:   "workspace-list",
		Short: "List durable Git-backed developer workspaces visible to the caller",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _, svc, err := openWorkspaceBackend(*stateDir, 0, 0, nil)
			if err != nil {
				return err
			}
			rows, err := svc.List(context.Background(), devworkspacedomain.Caller{Username: listActor}, func(string) bool { return true })
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), rows)
		},
	}
	wsListCmd.Flags().StringVar(&listActor, "actor", "operator", "operator identity")
	root.AddCommand(wsListCmd)

	var treeActor string
	wsTreeCmd := &cobra.Command{
		Use:   "workspace-tree <workspace-id> [prefix]",
		Short: "List committed Git files in the workspace /repo tree",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			prefix := "."
			if len(args) == 2 {
				prefix = args[1]
			}
			_, _, svc, err := openWorkspaceBackend(*stateDir, 0, 0, nil)
			if err != nil {
				return err
			}
			page, err := svc.Tree(context.Background(), devworkspacedomain.Caller{Username: treeActor}, args[0], prefix, "", 50, func(string) bool { return true })
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), page)
		},
	}
	wsTreeCmd.Flags().StringVar(&treeActor, "actor", "operator", "operator identity")
	root.AddCommand(wsTreeCmd)

	var fileActor, fileRevision string
	wsFileCmd := &cobra.Command{
		Use:   "workspace-file <workspace-id> <path>",
		Short: "Read one committed Git file from the workspace /repo tree",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, svc, err := openWorkspaceBackend(*stateDir, 0, 0, nil)
			if err != nil {
				return err
			}
			view, err := svc.File(context.Background(), devworkspacedomain.Caller{Username: fileActor}, args[0], args[1], fileRevision, func(string) bool { return true })
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), view)
		},
	}
	wsFileCmd.Flags().StringVar(&fileActor, "actor", "operator", "operator identity")
	wsFileCmd.Flags().StringVar(&fileRevision, "revision", "", "optional revision or blob digest")
	root.AddCommand(wsFileCmd)

	var syncActor string
	var syncWrites []string
	wsSyncCmd := &cobra.Command{
		Use:   "workspace-sync <workspace-id> [message]",
		Short: "Stage and commit /repo changes onto the workspace Git branch",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			msg := "chore(workspace): sync files"
			if len(args) == 2 {
				msg = args[1]
			}
			store, _, _, err := openWorkspaceBackend(*stateDir, 0, 0, nil)
			if err != nil {
				return err
			}
			ws, err := store.Get(context.Background(), args[0])
			if err != nil {
				return err
			}
			if ws.Owner != syncActor {
				return devworkspacedomain.ErrForbidden
			}
			if len(syncWrites) > 0 {
				files := make(map[string]string, len(syncWrites))
				for _, pair := range syncWrites {
					k, v, ok := strings.Cut(pair, "=")
					if !ok || strings.TrimSpace(k) == "" {
						return fmt.Errorf("--write %q must be path=content", pair)
					}
					files[strings.TrimSpace(k)] = v
				}
				updated, err := store.WriteAndCommit(context.Background(), args[0], files, msg)
				if err != nil {
					return err
				}
				return encodeJSON(cmd.OutOrStdout(), updated)
			}
			updated, err := store.Sync(context.Background(), args[0], msg)
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), updated)
		},
	}
	wsSyncCmd.Flags().StringVar(&syncActor, "actor", "operator", "operator identity owning the workspace")
	wsSyncCmd.Flags().StringArrayVar(&syncWrites, "write", nil, "write relative path=content inside /repo and commit")
	root.AddCommand(wsSyncCmd)

	var diffActor, diffRevision string
	wsDiffCmd := &cobra.Command{
		Use:   "workspace-diff <workspace-id>",
		Short: "Inspect the committed Git diff for a workspace branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, svc, err := openWorkspaceBackend(*stateDir, 0, 0, nil)
			if err != nil {
				return err
			}
			diff, err := svc.Diff(context.Background(), devworkspacedomain.Caller{Username: diffActor}, args[0], diffRevision, func(string) bool { return true })
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), diff)
		},
	}
	wsDiffCmd.Flags().StringVar(&diffActor, "actor", "operator", "operator identity")
	wsDiffCmd.Flags().StringVar(&diffRevision, "revision", "", "optional expected proposal revision")
	root.AddCommand(wsDiffCmd)

	var proposeActor, proposeTitle, proposeDesc string
	var proposeApprove bool
	wsProposeCmd := &cobra.Command{
		Use:   "workspace-propose <workspace-id>",
		Short: "Open a governed ChangeRequest from the workspace's committed Git revision",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, svc, err := openWorkspaceBackend(*stateDir, 0, 0, nil)
			if err != nil {
				return err
			}
			receipt, err := svc.Submit(
				context.Background(),
				devworkspacedomain.Caller{Username: proposeActor},
				args[0],
				proposeTitle,
				proposeDesc,
				proposeApprove,
			)
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), receipt)
		},
	}
	wsProposeCmd.Flags().StringVar(&proposeActor, "actor", "operator", "operator identity owning the workspace")
	wsProposeCmd.Flags().StringVar(&proposeTitle, "title", "", "change request title")
	wsProposeCmd.Flags().StringVar(&proposeDesc, "description", "", "change request description")
	wsProposeCmd.Flags().BoolVar(&proposeApprove, "approve", false, "attempt to self-approve (always refused by governance)")
	root.AddCommand(wsProposeCmd)

	var reapIdle, reapRetention time.Duration
	var reapNow string
	wsReapCmd := &cobra.Command{
		Use:   "workspace-reap",
		Short: "Reap idle workspaces, preserving dirty /repo commits and expiring elapsed volumes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			now := time.Now().UTC()
			if strings.TrimSpace(reapNow) != "" {
				parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(reapNow))
				if err != nil {
					return fmt.Errorf("invalid --now RFC3339 timestamp: %w", err)
				}
				now = parsed.UTC()
			}
			_, _, svc, err := openWorkspaceBackend(*stateDir, reapIdle, reapRetention, func() time.Time { return now })
			if err != nil {
				return err
			}
			reaped, err := svc.Reap(context.Background(), now)
			if err != nil {
				return err
			}
			return encodeJSON(cmd.OutOrStdout(), map[string]any{
				"reaped": reaped,
				"now":    now.Format(time.RFC3339),
			})
		},
	}
	wsReapCmd.Flags().DurationVar(&reapIdle, "idle-timeout", 30*time.Minute, "idle timeout before workspace compute is reclaimed")
	wsReapCmd.Flags().DurationVar(&reapRetention, "retention-window", 7*24*time.Hour, "retention window for volumes when branch push fails")
	wsReapCmd.Flags().StringVar(&reapNow, "now", "", "optional RFC3339 clock override for deterministic testing")
	root.AddCommand(wsReapCmd)

	var (
		mcpWorkspaceID string
		mcpListTools   bool
		mcpCallTool    string
		mcpCallArgs    string
	)
	mcpCmd := &cobra.Command{
		Use:   "mcp-serve [repo-path]",
		Short: "Serve the packaging toolbox (inspect_repo, read_file, validate_dockerfile, validate_manifests, dry_run_gates, inspect_image) over MCP",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			repoRoot := ""
			if len(args) == 1 {
				repoRoot = strings.TrimSpace(args[0])
			} else if strings.TrimSpace(mcpWorkspaceID) != "" {
				store, _, _, err := openWorkspaceBackend(*stateDir, 0, 0, nil)
				if err != nil {
					return err
				}
				if _, err := store.Get(ctx, strings.TrimSpace(mcpWorkspaceID)); err != nil {
					return err
				}
				repoRoot = store.RepoDir(strings.TrimSpace(mcpWorkspaceID))
			}
			var opts []packagingtools.Option
			if repoRoot != "" {
				opts = append(opts, packagingtools.WithRepoRoot(repoRoot))
			}
			server := packagingmcp.NewServer(packagingtools.New(opts...))

			if mcpListTools || strings.TrimSpace(mcpCallTool) != "" {
				serverTransport, clientTransport := mcp.NewInMemoryTransports()
				serverSession, err := server.Connect(ctx, serverTransport)
				if err != nil {
					return err
				}
				defer serverSession.Close()
				mcpClient := mcp.NewClient(&mcp.Implementation{Name: "packaging-workflow-cli", Version: "1.0.0"}, nil)
				clientSession, err := mcpClient.Connect(ctx, clientTransport, nil)
				if err != nil {
					return err
				}
				defer clientSession.Close()

				if mcpListTools {
					listed, err := clientSession.ListTools(ctx, nil)
					if err != nil {
						return err
					}
					return encodeJSON(cmd.OutOrStdout(), listed)
				}
				var callArgs map[string]any
				if strings.TrimSpace(mcpCallArgs) != "" {
					if err := json.Unmarshal([]byte(mcpCallArgs), &callArgs); err != nil {
						return fmt.Errorf("--args JSON: %w", err)
					}
				}
				res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
					Name:      strings.TrimSpace(mcpCallTool),
					Arguments: callArgs,
				})
				if err != nil {
					return err
				}
				return encodeJSON(cmd.OutOrStdout(), res)
			}

			return server.Run(ctx, &mcp.StdioTransport{})
		},
	}
	mcpCmd.Flags().StringVar(&mcpWorkspaceID, "workspace-id", "", "bind the MCP toolbox to a durable Git workspace /repo directory")
	mcpCmd.Flags().BoolVar(&mcpListTools, "list-tools", false, "query the MCP server for its registered tool schemas and print JSON")
	mcpCmd.Flags().StringVar(&mcpCallTool, "call", "", "invoke one MCP tool over an in-memory MCP session and print the result JSON")
	mcpCmd.Flags().StringVar(&mcpCallArgs, "args", "{}", "JSON object arguments for --call")
	root.AddCommand(mcpCmd)
}

func openWorkspaceBackend(stateDir string, idle, retention time.Duration, nowFn func() time.Time) (*gitstore.Store, controlplane.Repository, *devworkspaceapp.Service, error) {
	repository, err := controlplane.NewFileRepository(stateDir + "/plans")
	if err != nil {
		return nil, nil, nil, err
	}
	store, err := gitstore.Open(gitstore.Options{
		RootDir:         stateDir + "/workspaces",
		RetentionWindow: retention,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	gov := controlplane.NewGovernanceService(repository)
	svc := devworkspaceapp.New(devworkspaceapp.Dependencies{
		Records:   store,
		Files:     store.Tree(),
		Runtime:   store,
		Pods:      store.Pods(),
		Submitter: changerequest.New(gov),
		Auditor:   fileWorkspaceAuditor{repository: repository},
		Now:       nowFn,
		Idle:      idle,
	})
	return store, repository, svc, nil
}

type fileWorkspaceAuditor struct {
	repository controlplane.Repository
}

func (a fileWorkspaceAuditor) Record(ctx context.Context, subject, action, resource, decision, reason string) error {
	if a.repository == nil {
		return nil
	}
	dec := governance.DecisionAllow
	if strings.EqualFold(decision, "DENY") {
		dec = governance.DecisionDeny
	}
	event := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(),
		subject,
		action,
		resource,
		dec,
		reason,
		nil,
	)
	_, err := a.repository.AppendAuthorizationAudit(ctx, event)
	return err
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

func planningActivities(stateDir string, repository controlplane.Repository, useFixture bool) (planningworkflow.Activities, error) {
	var runner planningapp.Planner
	if raw := strings.TrimSpace(os.Getenv("CONDUIT_MODEL_CMD")); raw != "" {
		argv, err := localcmd.ParseArgv(raw)
		if err != nil {
			return planningworkflow.Activities{}, fmt.Errorf("CONDUIT_MODEL_CMD: %w", err)
		}
		runner = planner.Planner{Argv: argv, Timeout: 45 * time.Second, Clone: workspaceAwareClone(stateDir)}
	} else if useFixture {
		runner = fixturePlanningRunner{stateDir: stateDir}
	}
	return planningworkflow.Activities{
		Planner: runner,
		Auditor: fileAcceptAuditor{repository: repository},
	}, nil
}

type fixturePlanningRunner struct {
	stateDir string
}

func (f fixturePlanningRunner) Turn(ctx context.Context, session *planningdomain.Session, message string) (*planningapp.TurnResult, error) {
	profile := planningdomain.ProfileRef{
		Name:    planningdomain.ProfileOnboard,
		Version: packagingskill.Version,
		Hash:    packagingskill.SHA256(),
	}
	if session.Inspection == nil {
		inspection := &planningdomain.Inspection{
			Language:       "JavaScript",
			PackageManager: "npm",
			Kind:           "service",
			StartCommand:   "node server.js",
			Ports:          []int{8080},
			HasDockerfile:  false,
			Tree:           []string{"README.md", "package.json"},
			ToolCalls:      []string{"inspect_repo", "read_file"},
		}
		resolvedCommit := "3a317b550123456789abcdef0123456789abcdef"
		if strings.TrimSpace(f.stateDir) != "" && session != nil {
			if store, _, _, err := openWorkspaceBackend(f.stateDir, 0, 0, nil); err == nil {
				wsID := "wsp_" + session.ID
				if ws, wsErr := store.Get(ctx, wsID); wsErr == nil {
					if ws.BaseCommit != "" {
						resolvedCommit = ws.BaseCommit
					}
					tools := packagingtools.New(packagingtools.WithRepoRoot(store.RepoDir(wsID)))
					if realInspect, inspErr := tools.InspectRepo(ctx); inspErr == nil && len(realInspect.Tree) > 0 {
						ports := realInspect.Ports
						if len(ports) == 0 {
							ports = []int{8080}
						}
						lang := realInspect.Language
						if lang == "" {
							lang = "JavaScript"
						}
						pm := realInspect.PackageManager
						if pm == "" {
							pm = "npm"
						}
						kind := realInspect.Kind
						if kind == "" {
							kind = "service"
						}
						startCmd := realInspect.StartCommand
						if startCmd == "" {
							startCmd = "node server.js"
						}
						inspection = &planningdomain.Inspection{
							Language:       lang,
							PackageManager: pm,
							Kind:           kind,
							StartCommand:   startCmd,
							Ports:          ports,
							HasDockerfile:  realInspect.HasDockerfile,
							Tree:           realInspect.Tree,
							ToolCalls:      []string{"inspect_repo", "read_file"},
						}
					}
				}
			}
		}
		return &planningapp.TurnResult{
			Messages: []planningdomain.Message{{
				Role: planningdomain.RoleModel,
				Text: fmt.Sprintf("Inspected repository (%s / %s, %d file(s)). I have 1 question(s) before I can write the plan.", inspection.Language, inspection.PackageManager, len(inspection.Tree)),
			}},
			Inspection: inspection,
			NeedsInput: []planningdomain.Question{{
				ID:       "node_version",
				Name:     "node_version",
				Label:    "Which Node version?",
				Type:     planningdomain.QuestionSelect,
				Required: true,
				Options:  []string{"18", "20"},
			}},
			ResolvedCommit: resolvedCommit,
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
