package composition

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jonathanagustin/conduit/internal/modelconfig"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	devchange "github.com/jonathanagustin/conduit/internal/devworkspace/adapters/changerequest"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/deterministic"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/imagebuild"
	packagingmodelhttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/workspacepkg"
	packagingapp "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

func packagingPlanningComputer(repository controlplane.Repository, principal controlplane.Principal, getenv func(string) string, handlerOptions ...controlplane.HandlerOptions) planningapp.AppgenComputer {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return func(ctx context.Context, session *planningdomain.Session) error {
		if session == nil || session.Binding == nil {
			return planningdomain.ErrComputeBeforeAccept
		}
		if strings.TrimSpace(getenv("CONDUIT_IMAGE_BUILDER")) != "docker" {
			return planningdomain.ErrComputeNotConfigured
		}
		if _, err := exec.LookPath("docker"); err != nil {
			return fmt.Errorf("docker is required for the admitted image builder: install it and start its daemon")
		}
		if _, err := packagingtools.LookZarf(); err != nil {
			return fmt.Errorf("zarf is required for package lint and package create: install zarf v1.0.0 or set ZARF_BIN to that binary")
		}
		gitRoot := strings.TrimSpace(getenv("DEVWORKSPACE_GIT_ROOT"))
		if gitRoot == "" {
			return fmt.Errorf("DEVWORKSPACE_GIT_ROOT is required to store the governed workspace")
		}
		repoURL := strings.TrimSpace(session.Binding.Intake.RepoURL)
		if repoURL == "" {
			return fmt.Errorf("accepted intake has no repository")
		}
		writer, err := packagingWriter(getenv)
		if err != nil {
			return err
		}
		store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot})
		if err != nil {
			return err
		}
		registry, err := packagingRegistry(getenv)
		if err != nil {
			return err
		}
		if err := registryAnswers(registry); err != nil {
			return err
		}
		app := packagingAppName(repoURL)
		wsID := "wsp_" + session.ID
		tag := registry + "/" + app + ":local"
		runtime := "scratch"
		network := "none"
		base := ""
		if packagingSourceBuild(repoURL, getenv) {
			runtime = "node"
			network = strings.TrimSpace(getenv("CONDUIT_DOCKER_BUILD_NETWORK"))
			if network == "" {
				network = "default"
			}
			ref := strings.TrimSpace(getenv("CONDUIT_PACKAGING_BASE_IMAGE"))
			if ref == "" {
				ref = "node:22-bookworm"
			}
			pinned, err := imagebuild.ResolvePinned(ctx, "docker", ref)
			if err != nil {
				return err
			}
			base = pinned
		}
		if err := os.MkdirAll(filepath.Join(gitRoot, "logs"), 0o755); err != nil {
			return err
		}
		logFile, err := os.Create(filepath.Join(gitRoot, "logs", session.ID+".log"))
		if err != nil {
			return err
		}
		defer logFile.Close()
		engine := imagebuild.Docker{Tag: tag, Network: network}
		packager := packagingapp.WorkspacePackager{
			Writer: writer, Checks: workspacepkg.Checks{Zarf: packagingtools.ZarfRunner{Timeout: 20 * time.Minute, PlainHTTP: registryPlainHTTP(registry)}},
			Store: workspacepkg.Store{}, Builder: engine, Inspector: engine, LogWriter: logFile,
		}
		result, err := packager.Governed(ctx, packagingapp.GovernedRequest{
			Dir: store.RepoDir(wsID), RepoURL: repoURL, Ref: session.Binding.Intake.Ref,
			Commit: session.Binding.ResolvedCommit, Branch: "workspace/" + principal.Username + "/" + wsID,
			AppName: app, ImageTag: tag, Runtime: runtime, BaseImage: base, Network: network,
		})
		if err != nil {
			return err
		}
		if result.Outcome != string(packagingdomain.OutcomePass) || result.Error != "" {
			reason := result.Error
			if reason == "" {
				reason = result.Reason
			}
			return fmt.Errorf("governed packaging failed: %s", reason)
		}
		now := time.Now().UTC()
		if err := store.Save(ctx, devdomain.Touch(devdomain.Workspace{
			ID: wsID, ProfileID: session.Binding.Profile.Name, EnvironmentID: "dev",
			Owner: principal.Username, Title: "Workspace for " + session.ID,
			Status: devdomain.StatusRunning, RepoURL: repoURL,
			Branch:     "workspace/" + principal.Username + "/" + wsID,
			BaseCommit: session.Binding.ResolvedCommit, HeadCommit: result.Head,
			SourceDigest:     "sha256:" + session.Binding.Plan.SHA256,
			ProposalRevision: result.Head, ArtifactDigest: result.Audit.ImageDigest,
			ValidationOutcome: result.Outcome,
		}, now)); err != nil {
			return err
		}
		fmt.Fprintf(logFile, "pinned image digest %s\n", result.Audit.ImageDigest)
		if repository != nil && strings.TrimSpace(principal.Username) != "" {
			receipt, err := devchange.New(newGovernanceService(repository, handlerOptions...)).Submit(ctx, devWorkspaceCaller(principal), devdomain.Submission{
				WorkspaceID: wsID, EnvironmentID: "dev", Revision: result.Head,
				SourceDigest: result.Audit.ImageDigest,
				Title:        "Package " + app,
				Description:  "Governed package for " + repoURL + " pinned at " + result.Audit.ImageDigest,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(logFile, "change request %s %s\n", receipt.ChangeRequestID, receipt.Status)
		}
		return nil
	}
}

func packagingWriter(getenv func(string) string) (packagingapp.PackageWriter, error) {
	getenv = modelconfig.ForCapability(getenv, modelconfig.Packaging)
	if client, err := packagingModelHTTPClient(getenv); err != nil {
		return nil, err
	} else if client != nil {
		return packagingmodelhttp.Adapter{Client: client}, nil
	}
	return deterministic.Writer{}, nil
}

func packagingSourceBuild(repoURL string, getenv func(string) string) bool {
	switch strings.TrimSpace(getenv("CONDUIT_PACKAGING_BUILD")) {
	case "source":
		return true
	case "scratch":
		return false
	default:
		return strings.Contains(strings.ToLower(repoURL), "excalidraw")
	}
}

func packagingRegistry(getenv func(string) string) (string, error) {
	registry := strings.TrimSpace(getenv("CONDUIT_LOCAL_REGISTRY"))
	if registry == "" {
		registry = "127.0.0.1:5000"
	}
	if strings.Contains(registry, "://") || strings.Contains(registry, "/") {
		return "", fmt.Errorf("CONDUIT_LOCAL_REGISTRY must be host:port")
	}
	if _, _, err := net.SplitHostPort(registry); err != nil {
		return "", fmt.Errorf("CONDUIT_LOCAL_REGISTRY must be host:port")
	}
	return registry, nil
}

func registryAnswers(registry string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + registry + "/v2/")
	if err != nil {
		return fmt.Errorf("registry %s is not answering. Start a local registry, or set CONDUIT_LOCAL_REGISTRY to one that is already running", registry)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("registry %s returned %s", registry, resp.Status)
	}
	return nil
}

func registryPlainHTTP(registry string) bool {
	host, _, err := net.SplitHostPort(registry)
	if err != nil {
		host = registry
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func packagingAppName(repoURL string) string {
	name := strings.TrimSuffix(strings.TrimSpace(repoURL), ".git")
	name = strings.TrimRight(name, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteByte('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-")
	if len(cleaned) < 2 {
		return "fixture"
	}
	if len(cleaned) > 40 {
		cleaned = strings.Trim(cleaned[:40], "-")
	}
	return cleaned
}
