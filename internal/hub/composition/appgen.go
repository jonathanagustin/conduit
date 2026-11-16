package composition

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/modelcli"
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
	if getenv == nil {
		getenv = func(string) string { return "" }
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
	return modelcli.Runner{Argv: argv, Timeout: timeout}, nil
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
	service := application.Service{
		Generator: generator,
		Sandbox:   sbx,
		Publish:   pub,
		Handoff:   sourcefile.Writer{Dir: strings.TrimSpace(getenv("PACKAGING_FIXTURE_DIR"))},
		Audit:     appAudit{repository: repository},
		Store:     &application.Memory{},
	}
	if stored != nil {
		service.Attachments = appAttachments{stored: stored}
	}
	return service
}

func mountAppGeneration(
	legacy http.Handler, repository controlplane.Repository, options controlplane.HandlerOptions, generator appdomain.AppGenerator,
	stored *attachapp.Service,
) http.Handler {
	service := newAppGenerationService(repository, generator, stored, nil, nil, os.Getenv)
	return mountAppGenerationWithService(legacy, options, service)
}

func mountAppGenerationWithService(
	legacy http.Handler, options controlplane.HandlerOptions, service application.Service,
) http.Handler {
	mux := http.NewServeMux()
	apphttp.Register(mux, apphttp.Config{
		Service: service,
		Authorize: func(request *http.Request) error {
			if _, err := operatorClaims(request, options); err != nil {
				return err
			}
			return nil
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
		if session == nil || session.Binding == nil {
			return planningdomain.ErrComputeBeforeAccept
		}
		res, err := service.Generate(ctx, application.Input{
			Name:        session.Binding.Intake.AppName,
			Description: session.Binding.Intake.ProblemStatement,
			Confirm:     true,
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
			if err := os.WriteFile(filepath.Join(pkgDir, f.Path), []byte(f.Content), 0o600); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(pkgDir, ".dockerignore"), []byte(dockerIgnore), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "package-source.yaml"), []byte(res.PackagingSource), 0o600); err != nil {
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
				BaseCommit:        session.Binding.ResolvedCommit,
				HeadCommit:        session.Binding.ResolvedCommit,
				ArtifactDigest:    res.ImageDigest,
				ValidationOutcome: string(packagingdomain.OutcomePass),
			}, now)); err != nil {
				return err
			}
		}
		if repository != nil && strings.TrimSpace(principal.Username) != "" {
			submitter := devchange.New(controlplane.NewGovernanceService(repository))
			sourceDigest := res.ImageDigest
			if sourceDigest == "" {
				sourceDigest = res.Provenance.ResponseSHA256
			}
			if _, err := submitter.Submit(ctx, devWorkspaceCaller(principal), devdomain.Submission{
				WorkspaceID:   wsID,
				EnvironmentID: "dev",
				Revision:      session.Binding.Plan.SHA256,
				SourceDigest:  sourceDigest,
				Title:         "Generated application " + session.Binding.Intake.AppName,
				Description:   session.Binding.Intake.ProblemStatement,
			}); err != nil {
				return err
			}
		}
		return nil
	}
}

type appAudit struct {
	repository controlplane.Repository
}

func (audit appAudit) Record(ctx context.Context, event application.Audit) error {
	decision := governance.DecisionDeny
	if event.Outcome == appdomain.OutcomePass {
		decision = governance.DecisionAllow
	}
	record := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(), "app-generation", "app.generate", "app-generation/"+event.ID, decision, event.Reason,
		map[string]any{
			"adapter": event.Adapter, "promptSha256": event.PromptSHA256, "responseSha256": event.ResponseSHA256,
			"files": event.Files, "buildPassed": event.BuildPassed, "buildReason": event.BuildReason,
			"healthPassed": event.HealthPassed, "healthReason": event.HealthReason,
			"attachmentSha256": event.AttachmentSHA256,
		},
	)
	_, err := audit.repository.AppendAuthorizationAudit(ctx, record)
	return err
}
