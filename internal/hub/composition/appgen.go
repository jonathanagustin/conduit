package composition

import (
	"context"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/modelconfig"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/modelcli"
	appmodelhttp "github.com/jonathanagustin/conduit/internal/appgen/adapters/modelhttp"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/registry"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/sandbox"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/sourcefile"
	"github.com/jonathanagustin/conduit/internal/appgen/application"
	appdomain "github.com/jonathanagustin/conduit/internal/appgen/domain"
	apphttp "github.com/jonathanagustin/conduit/internal/appgen/httpapi"
	attachapp "github.com/jonathanagustin/conduit/internal/attachments/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	devchange "github.com/jonathanagustin/conduit/internal/devworkspace/adapters/changerequest"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/workspacepkg"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

func appGenerator(getenv func(string) string) (appdomain.AppGenerator, error) {
	getenv = modelconfig.ForCapability(getenv, modelconfig.Appgen)
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if client, err := modelHTTPClient(getenv); err != nil {
		return nil, err
	} else if client != nil {
		return appmodelhttp.Adapter{Client: client}, nil
	}
	raw := strings.TrimSpace(getenv("CONDUIT_MODEL_CMD"))
	if raw == "" {
		return recorded.Adapter{}, nil
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
	return modelcli.Runner{Argv: argv, Timeout: timeout, ModelName: configuredModelName(getenv)}, nil
}

func newAppGenerationService(
	repository controlplane.Repository, generator appdomain.AppGenerator, stored *attachapp.Service,
	customSandbox application.Sandbox, customPublish application.Publisher, getenv func(string) string,
) application.Service {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	registryAddress := strings.TrimSpace(getenv("CONDUIT_LOCAL_REGISTRY"))
	if registryAddress == "" {
		registryAddress = "127.0.0.1:5000"
	}
	sbx := customSandbox
	if sbx == nil {
		sbx = sandbox.Docker{}
	}
	pub := customPublish
	if pub == nil {
		pub = registry.DockerPublisher{Registry: registryAddress}
	}
	recordedAudit := appAudit{repository: repository}
	service := application.Service{
		Generator: generator,
		Sandbox:   sbx,
		Publish:   pub,
		Handoff:   sourcefile.Writer{Dir: strings.TrimSpace(getenv("PACKAGING_FIXTURE_DIR"))},
		Audit:     recordedAudit,
		Access:    recordedAudit,
		Store:     &application.Memory{},
		// configureAppGeneration replaces the store, the repair count and the
		// build time limit from the Hub options and environment.
		RepairAttempts: application.DefaultRepairAttempts,
	}
	if stored != nil {
		service.Attachments = appAttachments{stored: stored}
	}
	return service
}

// Environment variables configureAppGeneration reads.
const (
	envAppgenRepairAttempts = "CONDUIT_APPGEN_REPAIR_ATTEMPTS"
	envAppgenBuildTimeout   = "CONDUIT_APPGEN_BUILD_TIMEOUT"
)

// configureAppGeneration applies the operator's settings to a service built
// by newAppGenerationService: where results are kept, how many times a failed
// reply is sent back to the model, and how long one build may run. A value
// that does not parse is a startup error. It also reports, once, when this
// deployment cannot build at all.
func configureAppGeneration(
	service application.Service, store application.Store, customSandbox application.Sandbox, getenv func(string) string,
) (application.Service, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if raw := strings.TrimSpace(getenv(envAppgenRepairAttempts)); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 || parsed > application.MaxRepairAttempts {
			return application.Service{}, fmt.Errorf("%s must be a number from 0 to %d", envAppgenRepairAttempts, application.MaxRepairAttempts)
		}
		service.RepairAttempts = parsed
	}
	if raw := strings.TrimSpace(getenv(envAppgenBuildTimeout)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return application.Service{}, fmt.Errorf("%s must be a positive duration", envAppgenBuildTimeout)
		}
		if customSandbox == nil {
			service.Sandbox = sandbox.Docker{BuildTimeout: parsed}
		}
	}
	// The executable opens the durable store and owns its close. With none
	// supplied, results stay in process memory and a restart loses them.
	if store == nil {
		store = &application.Memory{}
	}
	service.Store = store
	if status := appGenerationBuildExecutor(service.Sandbox); !status.Available {
		slog.Warn("app generation cannot build in this deployment; POST /v1/app-generations answers 503",
			"reason", status.Detail, "remedy", "run the Hub where the docker binary, and go for main.go and index.html, exist; the shipped FROM scratch image has neither")
	}
	return service, nil
}

// AppGenerationBuildExecutorStatus says whether this Hub can build a
// generated application, for an admin status surface.
type AppGenerationBuildExecutorStatus struct {
	Available bool
	// Detail says what is missing. It is empty when Available is true.
	Detail string
}

// AppGenerationBuildExecutor reports on the sandbox NewHub uses when no other
// is supplied: the container engine client on the Hub host.
func AppGenerationBuildExecutor() AppGenerationBuildExecutorStatus {
	return appGenerationBuildExecutor(sandbox.Docker{})
}

func appGenerationBuildExecutor(sbx application.Sandbox) AppGenerationBuildExecutorStatus {
	if sbx == nil {
		return AppGenerationBuildExecutorStatus{Detail: "no sandbox is configured"}
	}
	if checker, ok := sbx.(application.Availability); ok {
		if err := checker.Available(); err != nil {
			return AppGenerationBuildExecutorStatus{Detail: err.Error()}
		}
	}
	return AppGenerationBuildExecutorStatus{Available: true}
}

func mountAppGenerationWithService(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, service application.Service,
) http.Handler {
	governanceService := controlplane.NewGovernanceService(repository)
	mux := http.NewServeMux()
	apphttp.Register(mux, apphttp.Config{
		Service: service,
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return err
			}
			return nil
		},
		Operator: func(request *http.Request) (string, error) {
			principal, ok := packagingPrincipal(request, governanceService, options)
			if !ok || strings.TrimSpace(principal.Username) == "" {
				return "", apphttp.ErrUnauthorized
			}
			return principal.Username, nil
		},
	})
	mux.Handle("/", legacy)
	return mux
}

func appgenPlanningComputer(
	service application.Service, repository controlplane.Repository, principal controlplane.Principal, getenv func(string) string,
) planningapp.AppgenComputer {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return func(ctx context.Context, session *planningdomain.Session) error {
		contract, err := planningdomain.AcceptedGenerationFrom(session)
		if err != nil {
			return err
		}
		conversation := make([]appdomain.ConversationMessage, 0, len(contract.Conversation))
		for _, message := range contract.Conversation {
			if strings.TrimSpace(message.Text) == "" {
				continue
			}
			conversation = append(conversation, appdomain.ConversationMessage{Role: message.Role, Text: message.Text})
		}
		res, err := service.Generate(ctx, application.Input{
			Actor:          strings.TrimSpace(principal.Username),
			Name:           session.Binding.Intake.AppName,
			Description:    session.Binding.Intake.ProblemStatement,
			Confirm:        true,
			PlanRevisionID: contract.RevisionID,
			PlanDigest:     contract.Digest,
			AcceptedPlan:   contract.PlanText,
			Conversation:   conversation,
		})
		if err != nil {
			return err
		}
		if res.Error != "" {
			return fmt.Errorf("appgen gate failed: %s", res.Error)
		}
		for _, g := range res.Gates {
			if g.Outcome != appdomain.OutcomePass {
				return fmt.Errorf("appgen gate %s: %s", g.Name, g.Reason)
			}
		}
		filesByName := make(map[string]string, len(res.Files))
		for _, f := range res.Files {
			filesByName[f.Path] = f.Content
		}
		const dockerIgnore = ".git\nout\n*.tar.zst\n.env\n*.pem\n*.key\n"
		checks := workspacepkg.Checks{}
		if reason := checks.Dockerfile(filesByName["Dockerfile"], dockerIgnore); reason != "" {
			return fmt.Errorf("packaging dockerfile gate: %s", reason)
		}
		if strings.TrimSpace(filesByName["zarf.yaml"]) == "" {
			return fmt.Errorf("packaging zarf.yaml gate: missing zarf.yaml")
		}
		if reason := checks.PackageCR(filesByName["uds-package.yaml"]); reason != "" {
			return fmt.Errorf("packaging uds-package.yaml gate: %s", reason)
		}
		wsID := "wsp_" + session.ID
		var (
			store  *gitstore.Store
			pkgDir string
		)
		if gitRoot := strings.TrimSpace(getenv("DEVWORKSPACE_GIT_ROOT")); gitRoot != "" {
			var openErr error
			store, openErr = gitstore.Open(gitstore.Options{RootDir: gitRoot})
			if openErr != nil {
				return openErr
			}
			pkgDir = store.RepoDir(wsID)
			if err := os.MkdirAll(pkgDir, 0o755); err != nil {
				return err
			}
		} else {
			tmpDir, err := os.MkdirTemp("", "conduit-appgen-pkg-*")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()
			pkgDir = tmpDir
		}
		for _, f := range res.Files {
			if err := writePackageFile(pkgDir, f.Path, f.Content); err != nil {
				return err
			}
		}
		if err := writePackageFile(pkgDir, ".dockerignore", dockerIgnore); err != nil {
			return err
		}
		if err := writePackageFile(pkgDir, "package-source.yaml", res.PackagingSource); err != nil {
			return err
		}
		if err := checks.Lint(ctx, pkgDir); err != nil {
			return fmt.Errorf("packaging zarf lint gate: %w", err)
		}
		if archive, err := checks.Create(ctx, pkgDir, filepath.Join(pkgDir, "out")); err != nil || strings.TrimSpace(archive) == "" {
			if err != nil {
				return fmt.Errorf("packaging zarf create gate: %w", err)
			}
			return fmt.Errorf("packaging zarf create gate: archive was not created")
		}
		if store != nil {
			now := time.Now().UTC()
			if err := store.Save(ctx, devdomain.Touch(devdomain.Workspace{
				ID:                wsID,
				ProfileID:         session.Binding.Profile.Name,
				EnvironmentID:     "dev",
				Owner:             principal.Username,
				Title:             "Workspace for " + session.ID,
				Status:            devdomain.StatusRunning,
				RepoURL:           "https://conduit.local/appgen/" + session.Binding.Intake.AppName,
				Branch:            "workspace/" + principal.Username + "/" + wsID,
				ArtifactDigest:    res.ImageDigest,
				ValidationOutcome: string(packagingdomain.OutcomePass),
			}, now)); err != nil {
				return err
			}
			toCommit := make(map[string]string, len(res.Files)+2)
			for _, f := range res.Files {
				toCommit[f.Path] = f.Content
			}
			toCommit[".dockerignore"] = dockerIgnore
			toCommit["package-source.yaml"] = res.PackagingSource
			if _, err := store.WriteAndCommit(ctx, wsID, toCommit, "feat(appgen): package "+session.Binding.Intake.AppName); err != nil {
				return err
			}
		}
		if repository != nil && strings.TrimSpace(principal.Username) != "" {
			submitter := devchange.New(controlplane.NewGovernanceService(repository))
			sourceDigest := res.ImageDigest
			if sourceDigest == "" {
				sourceDigest = res.Provenance.ResponseSHA256
			}
			receipt, err := submitter.Submit(ctx, devWorkspaceCaller(principal), devdomain.Submission{
				WorkspaceID:   wsID,
				EnvironmentID: "dev",
				Revision:      session.Binding.Plan.SHA256,
				SourceDigest:  sourceDigest,
				Title:         "Generated application " + session.Binding.Intake.AppName,
				Description:   session.Binding.Intake.ProblemStatement,
			})
			if err != nil {
				return err
			}
			if store != nil && strings.TrimSpace(receipt.ChangeRequestID) != "" {
				if ws, getErr := store.Get(ctx, wsID); getErr == nil {
					ws.ApprovalID = receipt.ChangeRequestID
					if saveErr := store.Save(ctx, ws); saveErr != nil {
						return saveErr
					}
				}
			}
		}
		return nil
	}
}

func writePackageFile(root, rel, content string) error {
	cleaned := filepath.Clean(rel)
	if cleaned == "." || cleaned == "" || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("package path %s is not allowed", rel)
	}
	dest := filepath.Join(root, cleaned)
	rootClean := filepath.Clean(root)
	if dest != rootClean && !strings.HasPrefix(dest, rootClean+string(filepath.Separator)) {
		return fmt.Errorf("package path %s is not allowed", rel)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, []byte(content), 0o600)
}

type appAudit struct {
	repository controlplane.Repository
}

func (audit appAudit) Deny(ctx context.Context, event application.Denial) error {
	if audit.repository == nil {
		return nil
	}
	actor := strings.TrimSpace(event.Actor)
	if actor == "" {
		actor = "unknown"
	}
	record := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), actor, event.Action, "app-generation/"+event.ID, governance.DecisionDeny, event.Reason, nil,
	)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, record)
	return err
}

func (audit appAudit) Record(ctx context.Context, event application.Audit) error {
	decision := governance.DecisionDeny
	if event.Outcome == appdomain.OutcomePass {
		decision = governance.DecisionAllow
	}
	record := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), "app-generation", "app.generate", "app-generation/"+event.ID, decision, event.Reason,
		map[string]any{
			"attempt": event.Attempt, "planRevisionId": event.PlanRevisionID, "planDigest": event.PlanDigest,
			"adapter": event.Adapter, "configuredModel": event.ConfiguredModel, "providerModel": event.ProviderModel, "promptSha256": event.PromptSHA256, "responseSha256": event.ResponseSHA256,
			"files": event.Files, "buildPassed": event.BuildPassed, "buildReason": event.BuildReason,
			"healthPassed": event.HealthPassed, "healthReason": event.HealthReason,
			"attachmentSha256": event.AttachmentSHA256,
		},
	)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, record)
	return err
}
