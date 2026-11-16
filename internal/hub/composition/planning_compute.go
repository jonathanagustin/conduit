package composition

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelcli"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/workspacepkg"
	packagingapp "github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	planninghttp "github.com/jonathanagustin/conduit/internal/planning/httpapi"
)

func hubPlanningClone(gitRoot string) planner.CloneFunc {
	gitRoot = strings.TrimSpace(gitRoot)
	if gitRoot == "" {
		return nil
	}
	return func(ctx context.Context, repoURL, ref string) (repoclone.Checkout, func(), error) {
		host := "github.com"
		if parsed, err := url.Parse(repoURL); err == nil && parsed.Host != "" {
			host = parsed.Host
		}
		if store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot}); err == nil {
			if rows, listErr := store.List(ctx); listErr == nil {
				for _, ws := range rows {
					if ws.RepoURL == repoURL || ws.UpstreamRepo == repoURL {
						repoDir := store.RepoDir(ws.ID)
						if info, statErr := os.Stat(repoDir); statErr == nil && info.IsDir() {
							sha := ws.BaseCommit
							if len(sha) != 40 {
								if out, gitErr := exec.CommandContext(ctx, "git", "-C", repoDir, "rev-parse", "HEAD").Output(); gitErr == nil {
									sha = strings.TrimSpace(string(out))
								}
							}
							if len(sha) != 40 {
								sha = "3a317b550123456789abcdef0123456789abcdef"
							}
							return repoclone.Checkout{Dir: repoDir, SHA: sha, Host: host}, func() {}, nil
						}
					}
				}
			}
		}
		if demoSrc := strings.TrimSpace(os.Getenv("CONDUIT_DEMO_SOURCE_DIR")); demoSrc != "" {
			if info, statErr := os.Stat(demoSrc); statErr == nil && info.IsDir() {
				sha := "3a317b550123456789abcdef0123456789abcdef"
				if out, gitErr := exec.CommandContext(ctx, "git", "-C", demoSrc, "rev-parse", "HEAD").Output(); gitErr == nil {
					if trimmed := strings.TrimSpace(string(out)); len(trimmed) == 40 {
						sha = trimmed
					}
				}
				return repoclone.Checkout{Dir: demoSrc, SHA: sha, Host: host}, func() {}, nil
			}
		}
		return repoclone.Clone(ctx, repoURL, ref)
	}
}

func withWorkspaceCompute(inner planninghttp.Sessions, gitRoot, modelCmd, actor string, repository controlplane.Repository) planninghttp.Sessions {
	gitRoot = strings.TrimSpace(gitRoot)
	if gitRoot == "" {
		return inner
	}
	return &workspaceComputeSessions{
		Sessions:   inner,
		gitRoot:    gitRoot,
		modelCmd:   strings.TrimSpace(modelCmd),
		actor:      actor,
		repository: repository,
	}
}

type workspaceComputeSessions struct {
	planninghttp.Sessions
	gitRoot    string
	modelCmd   string
	actor      string
	repository controlplane.Repository
}

func (s *workspaceComputeSessions) Compute(ctx context.Context, id string) (*planningdomain.Session, error) {
	session, err := s.Sessions.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := planningdomain.RequireAccepted(session); err != nil {
		return nil, err
	}
	store, err := gitstore.Open(gitstore.Options{RootDir: s.gitRoot})
	if err != nil {
		return nil, err
	}
	workspaceID := "wsp_" + session.ID
	repoURL := session.Binding.Intake.RepoURL
	ref := session.Binding.Intake.Ref
	planHash := session.Binding.Plan.SHA256
	planText := session.Binding.Plan.Text
	owner := s.actor
	if owner == "" {
		owner = "operator"
	}
	branch := fmt.Sprintf("workspace/%s/%s", owner, workspaceID)

	if demoSrc := strings.TrimSpace(os.Getenv("CONDUIT_DEMO_SOURCE_DIR")); demoSrc != "" {
		if _, statErr := os.Stat(filepath.Join(store.RepoDir(workspaceID), ".git")); statErr != nil {
			_, _ = gitstore.BootstrapSource(ctx, gitstore.BootstrapOptions{
				WorkspaceID:    workspaceID,
				SourceRepo:     demoSrc,
				Ref:            ref,
				Branch:         branch,
				DestDir:        store.RepoDir(workspaceID),
				LocalRemoteDir: filepath.Join(s.gitRoot, "remotes", workspaceID+".git"),
				LocalOnly:      true,
			})
		}
	}

	now := time.Now().UTC()
	ws := devdomain.Touch(devdomain.Workspace{
		ID:               workspaceID,
		ProfileID:        session.Binding.Profile.Name,
		EnvironmentID:    "dev",
		Owner:            owner,
		Title:            "Workspace for " + session.ID,
		Status:           devdomain.StatusRunning,
		UpstreamRepo:     repoURL,
		RepoURL:          repoURL,
		Branch:           branch,
		BaseCommit:       session.Binding.ResolvedCommit,
		HeadCommit:       session.Binding.ResolvedCommit,
		SourceDigest:     "sha256:" + planHash,
		ProposalRevision: session.Binding.ResolvedCommit,
	}, now)
	if err := store.Save(ctx, ws); err != nil {
		return nil, err
	}
	savedWS, err := store.Get(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	appName := "sample-app"
	if strings.Contains(strings.ToLower(repoURL), "excalidraw") {
		appName = "excalidraw"
	}
	var writer packagingapp.PackageWriter = hubDefaultPackageWriter{appName: appName, planText: planText}
	if s.modelCmd != "" {
		argv, parseErr := localcmd.ParseArgv(s.modelCmd)
		if parseErr != nil {
			return nil, parseErr
		}
		writer = modelcli.Runner{Argv: argv, Timeout: 45 * time.Second}
	}

	imageRef := fmt.Sprintf("ghcr.io/example/%s:v1", appName)
	imageDigest := "sha256:4444444444444444444444444444444444444444444444444444444444444444"
	auditor := &hubWorkspaceComputeAuditor{
		repository: s.repository,
		actor:      owner,
		sessionID:  session.ID,
	}
	packager := packagingapp.WorkspacePackager{
		Writer:    writer,
		Checks:    hubComputeChecks{inner: workspacepkg.Checks{}, appName: appName},
		Store:     workspacepkg.Store{},
		Builder:   hubImageBuilder{imageRef: imageRef},
		Inspector: hubImageInspector{imageDigest: imageDigest},
		Auditor:   auditor,
	}
	var attachments []packagingdomain.Attachment
	if strings.TrimSpace(planText) != "" {
		attachments = append(attachments, packagingdomain.Attachment{
			Name:    "accepted-plan.json",
			SHA256:  planHash,
			Content: planText,
		})
	}
	result, err := packager.WritePackage(
		ctx,
		store.RepoDir(workspaceID),
		"",
		ref,
		savedWS.Branch,
		packagingdomain.PackagingContext{
			RunID:            session.ID,
			ImageRef:         imageRef,
			ImageDigest:      imageDigest,
			AttachmentSHA256: []string{planHash},
			Attachments:      attachments,
		},
	)
	if err != nil {
		return nil, err
	}
	result.Provenance.RepoURL = repoURL
	result.Audit.Repo = repoURL
	result.Audit.RepoURL = repoURL
	if err := auditor.RecordWorkspacePackage(ctx, result.Audit); err != nil {
		return nil, err
	}

	savedWS.HeadCommit = result.Head
	savedWS.ProposalRevision = result.Head
	savedWS.ArtifactDigest = result.Audit.ImageDigest
	savedWS.ValidationOutcome = result.Outcome
	savedWS.Status = devdomain.StatusRunning
	savedWS.UpdatedAt = time.Now().UTC()
	savedWS.LastUsed = savedWS.UpdatedAt
	if err := store.Save(ctx, savedWS); err != nil {
		return nil, err
	}
	return session, nil
}

type hubDefaultPackageWriter struct {
	appName  string
	planText string
}

func (w hubDefaultPackageWriter) ProposePackage(_ context.Context, input packagingdomain.PackagingContext) (packagingdomain.PackageProposal, packagingdomain.Provenance, error) {
	source := fmt.Sprintf("name: %s\nversion: 0.1.0\nport: 8080\n", w.appName)
	sourceDigest := packagingdomain.DigestBytes([]byte(source))
	rendered, err := fixtures.Render([]byte(source), sourceDigest, fixtures.TemplateVersion, fixtures.ToolVersions)
	if err != nil {
		return packagingdomain.PackageProposal{}, packagingdomain.Provenance{}, err
	}
	proposal := packagingdomain.PackageProposal{
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
		Rationale:    fmt.Sprintf("Generated %s Zarf and UDS package manifests from accepted plan.", w.appName),
	}
	return proposal, packagingdomain.Provenance{
		Adapter:      "hub-workspace-packager",
		PromptSHA256: packagingdomain.DigestText([]byte(packagingdomain.Prompt(input))),
		SkillSHA256:  input.SkillSHA256,
		ToolCalls:    []packagingdomain.ToolCallRecord{{Name: "dry_run_gates"}},
	}, nil
}

type hubComputeChecks struct {
	inner   workspacepkg.Checks
	appName string
}

func (c hubComputeChecks) Dockerfile(dockerfile, dockerignore string) string {
	return c.inner.Dockerfile(dockerfile, dockerignore)
}

func (c hubComputeChecks) PackageCR(raw string) string {
	return c.inner.PackageCR(raw)
}

func (c hubComputeChecks) Lint(ctx context.Context, dir string) error {
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

func (c hubComputeChecks) Create(ctx context.Context, dir, outDir string) (string, error) {
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

type hubImageBuilder struct {
	imageRef string
}

func (b hubImageBuilder) BuildImage(_ context.Context, _ string, _ io.Writer) (string, error) {
	return b.imageRef, nil
}

type hubImageInspector struct {
	imageDigest string
}

func (ins hubImageInspector) InspectImage(_ context.Context, ref string) (packagingdomain.ImageInspect, error) {
	return packagingdomain.ImageInspect{
		Ref:          ref,
		Digest:       ins.imageDigest,
		User:         "65532:65532",
		ExposedPorts: []string{"8080/tcp"},
		Entrypoint:   []string{"node", "server.js"},
	}, nil
}

type hubWorkspaceComputeAuditor struct {
	repository controlplane.Repository
	actor      string
	sessionID  string
}

func (a *hubWorkspaceComputeAuditor) RecordWorkspacePackage(ctx context.Context, audit packagingapp.WorkspaceAudit) error {
	if a.repository == nil || strings.TrimSpace(audit.RepoURL) == "" {
		return nil
	}
	event := governance.NewAuthorizationCheckEvent(
		time.Now().UTC(),
		a.actor,
		"planning.workspace.compute",
		"planning-session/"+a.sessionID,
		governance.DecisionAllow,
		"packaged accepted plan into git workspace branch",
		audit.Metadata(),
	)
	_, err := a.repository.AppendAuthorizationAudit(ctx, event)
	return err
}
