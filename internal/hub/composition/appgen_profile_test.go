package composition

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/sourcefile"
	appgenapp "github.com/jonathanagustin/conduit/internal/appgen/application"
	appgendomain "github.com/jonathanagustin/conduit/internal/appgen/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/workspacepkg"
	packagingdomain "github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	appgenskill "github.com/jonathanagustin/conduit/skills/appgen"
)

type testAppgenSandbox struct {
	builds int
}

func (s *testAppgenSandbox) Build(_ context.Context, _ appgenapp.BuildRequest) (appgenapp.BuildResult, error) {
	s.builds++
	return appgenapp.BuildResult{Passed: true, Reason: "image built without network", ImageID: "sha256:appgen"}, nil
}

func (s *testAppgenSandbox) Check(_ context.Context, _ appgenapp.BuildResult, _ appgenapp.BuildRequest) (appgenapp.HealthResult, error) {
	return appgenapp.HealthResult{Passed: true, Reason: "GET /health returned 200"}, nil
}

type testAppgenPublisher struct {
	pushes int
}

func (p *testAppgenPublisher) Push(_ context.Context, _, name string, _ []byte) (string, string, string, error) {
	p.pushes++
	return "127.0.0.1:5000/" + name + ":0.1.0", "sha256:" + strings.Repeat("a", 64), "sha256:" + strings.Repeat("b", 64), nil
}

func TestAppgenProfileStartTurnReadyAcceptAndBinding(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	handler := planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, nil)

	createBody := `{"profile":"appgen","problemStatement":"Build an HTTP status dashboard for cluster health","appName":"cluster-status"}`
	resp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", createBody)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create appgen session: %d %s", resp.Code, resp.Body.String())
	}
	var session planningdomain.Session
	if err := json.Unmarshal(resp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Profile.Name != planningdomain.ProfileAppgen {
		t.Fatalf("expected profile appgen, got %+v", session.Profile)
	}
	if len(session.NeedsInput) != 0 {
		t.Fatalf("expected no NeedsInput when problemStatement and appName are supplied, got %+v", session.NeedsInput)
	}

	turnResp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/turns", `{"message":"include a /health endpoint"}`)
	if turnResp.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", turnResp.Code, turnResp.Body.String())
	}
	if err := json.Unmarshal(turnResp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != planningdomain.StatusReady || session.Plan == nil || session.Plan.Text == "" {
		t.Fatalf("expected ready session with plan, got %+v", session)
	}
	if session.ResolvedCommit != planningdomain.NoRepositoryCommit {
		t.Fatalf("expected resolvedCommit %q, got %q", planningdomain.NoRepositoryCommit, session.ResolvedCommit)
	}

	acceptResp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/accept", `{}`)
	if acceptResp.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", acceptResp.Code, acceptResp.Body.String())
	}
	if err := json.Unmarshal(acceptResp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Binding == nil {
		t.Fatal("expected binding on accepted session")
	}
	if session.Binding.Profile.Name != planningdomain.ProfileAppgen || session.Binding.Profile.Hash != appgenskill.SHA256() {
		t.Fatalf("unexpected bound profile: %+v", session.Binding.Profile)
	}
	if session.Binding.ResolvedCommit != planningdomain.NoRepositoryCommit {
		t.Fatalf("unexpected bound resolvedCommit: %q", session.Binding.ResolvedCommit)
	}
	if session.Binding.Intake.ProblemStatement != "Build an HTTP status dashboard for cluster health" || session.Binding.Intake.AppName != "cluster-status" {
		t.Fatalf("unexpected bound intake: %+v", session.Binding.Intake)
	}
}

func TestAppgenProfileMissingAppNameRaisesNeedsInput(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	handler := planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, nil)

	createBody := `{"profile":"appgen","problemStatement":"Build an incident timeline viewer"}`
	resp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", createBody)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.Code, resp.Body.String())
	}
	var session planningdomain.Session
	if err := json.Unmarshal(resp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if len(session.NeedsInput) != 1 || session.NeedsInput[0].ID != "app_name" {
		t.Fatalf("expected NeedsInput for app_name, got %+v", session.NeedsInput)
	}

	turnResp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/turns", `{}`)
	if turnResp.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", turnResp.Code, turnResp.Body.String())
	}
	if err := json.Unmarshal(turnResp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != planningdomain.StatusNeedsInput || session.Plan != nil {
		t.Fatalf("expected needs_input without plan before answering app_name, got %+v", session)
	}

	ansResp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/answers", `{"answers":[{"id":"app_name","value":"incident-timeline"}]}`)
	if ansResp.Code != http.StatusOK {
		t.Fatalf("answers: %d %s", ansResp.Code, ansResp.Body.String())
	}

	turnResp2 := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions/"+session.ID+"/turns", `{}`)
	if turnResp2.Code != http.StatusOK {
		t.Fatalf("turn after answer: %d %s", turnResp2.Code, turnResp2.Body.String())
	}
	if err := json.Unmarshal(turnResp2.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.Status != planningdomain.StatusReady || session.Plan == nil || session.Intake.AppName != "incident-timeline" {
		t.Fatalf("expected ready session with updated appName, got %+v", session)
	}
}

func TestAppgenProfileRefusesComputeBeforeAcceptAndGeneratesBeforePackaging(t *testing.T) {
	gitRoot := t.TempDir()
	handoffDir := t.TempDir()
	sandbox := &testAppgenSandbox{}
	publisher := &testAppgenPublisher{}
	repository := controlplane.NewMemoryRepository()
	appgenSvc := appgenapp.Service{
		Generator: recorded.Adapter{},
		Sandbox:   sandbox,
		Publish:   publisher,
		Handoff:   sourcefile.Writer{Dir: handoffDir},
		Audit:     appAudit{repository: repository},
		Store:     &appgenapp.Memory{},
	}
	handler := planningHandlerWithAppgenCompute(
		http.NotFoundHandler(),
		repository,
		controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true},
		nil,
		func(ctx context.Context, s *planningdomain.Session) error {
			res, err := appgenSvc.Generate(ctx, appgenapp.Input{
				Name:        s.Binding.Intake.AppName,
				Description: s.Binding.Intake.ProblemStatement,
				Confirm:     true,
			})
			if err != nil {
				return err
			}
			if res.Error != "" {
				return fmt.Errorf("appgen gate failed: %s", res.Error)
			}
			for _, g := range res.Gates {
				if g.Outcome != appgendomain.OutcomePass {
					return fmt.Errorf("gate %s: %s", g.Name, g.Reason)
				}
			}
			store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot})
			if err != nil {
				return err
			}
			wsID := "wsp_" + s.ID
			repoDir := store.RepoDir(wsID)
			if err := os.MkdirAll(repoDir, 0o755); err != nil {
				return err
			}
			for _, f := range res.Files {
				if err := os.WriteFile(filepath.Join(repoDir, f.Path), []byte(f.Content), 0o600); err != nil {
					return err
				}
			}
			if err := os.WriteFile(filepath.Join(repoDir, ".dockerignore"), []byte(".git\nout\n*.tar.zst\n.env\n*.pem\n*.key\n"), 0o600); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(repoDir, "package-source.yaml"), []byte(res.PackagingSource), 0o600); err != nil {
				return err
			}
			now := time.Now().UTC()
			return store.Save(ctx, devdomain.Touch(devdomain.Workspace{
				ID:                wsID,
				ProfileID:         s.Binding.Profile.Name,
				EnvironmentID:     "dev",
				Owner:             "alice",
				Title:             "Workspace for " + s.ID,
				Status:            devdomain.StatusRunning,
				RepoURL:           "https://conduit.local/appgen/" + s.Binding.Intake.AppName,
				Branch:            "workspace/alice/" + wsID,
				BaseCommit:        s.Binding.ResolvedCommit,
				HeadCommit:        s.Binding.ResolvedCommit,
				ArtifactDigest:    res.ImageDigest,
				ValidationOutcome: string(packagingdomain.OutcomePass),
			}, now))
		},
	)

	createBody := `{"profile":"appgen","problemStatement":"Create a service catalog portal","appName":"hello-http"}`
	resp := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", createBody)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", resp.Code, resp.Body.String())
	}
	var session planningdomain.Session
	if err := json.Unmarshal(resp.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	path := "/v1/planning-sessions/" + session.ID

	// No generation or packaging runs before Accept.
	preAccept := planningRequest(t, handler, "alice", http.MethodPost, path+"/compute", `{}`)
	if preAccept.Code != http.StatusConflict || !strings.Contains(preAccept.Body.String(), "COMPUTE_BEFORE_ACCEPT") {
		t.Fatalf("expected 409 COMPUTE_BEFORE_ACCEPT before turn/accept, got %d %s", preAccept.Code, preAccept.Body.String())
	}
	if sandbox.builds != 0 || publisher.pushes != 0 {
		t.Fatalf("sandbox/publisher should not run before Accept, got builds=%d pushes=%d", sandbox.builds, publisher.pushes)
	}

	if r := planningRequest(t, handler, "alice", http.MethodPost, path+"/turns", `{}`); r.Code != http.StatusOK {
		t.Fatalf("turn: %d %s", r.Code, r.Body.String())
	}
	preAcceptAfterTurn := planningRequest(t, handler, "alice", http.MethodPost, path+"/compute", `{}`)
	if preAcceptAfterTurn.Code != http.StatusConflict || !strings.Contains(preAcceptAfterTurn.Body.String(), "COMPUTE_BEFORE_ACCEPT") {
		t.Fatalf("expected 409 COMPUTE_BEFORE_ACCEPT after turn before accept, got %d %s", preAcceptAfterTurn.Code, preAcceptAfterTurn.Body.String())
	}
	if sandbox.builds != 0 || publisher.pushes != 0 {
		t.Fatalf("sandbox/publisher should not run before Accept, got builds=%d pushes=%d", sandbox.builds, publisher.pushes)
	}

	if r := planningRequest(t, handler, "alice", http.MethodPost, path+"/accept", `{}`); r.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", r.Code, r.Body.String())
	}
	computeResp := planningRequest(t, handler, "alice", http.MethodPost, path+"/compute", `{}`)
	if computeResp.Code != http.StatusAccepted {
		t.Fatalf("compute after accept: %d %s", computeResp.Code, computeResp.Body.String())
	}
	if sandbox.builds != 1 || publisher.pushes != 1 {
		t.Fatalf("expected 1 build and 1 push after Accept+Compute, got builds=%d pushes=%d", sandbox.builds, publisher.pushes)
	}

	// Verify both generated application files and packaged manifests exist in the workspace and handoff dir.
	if _, err := os.Stat(filepath.Join(handoffDir, "sources", "hello-http-1.txt")); err != nil {
		t.Fatalf("missing packaging handoff source file: %v", err)
	}
	store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot})
	if err != nil {
		t.Fatal(err)
	}
	repoDir := store.RepoDir("wsp_" + session.ID)
	for _, wantFile := range []string{"main.go", "values.yaml", "Dockerfile", ".dockerignore", "zarf.yaml", "uds-package.yaml", "package-source.yaml"} {
		if _, err := os.Stat(filepath.Join(repoDir, wantFile)); err != nil {
			t.Fatalf("missing expected generated/packaged file %s: %v", wantFile, err)
		}
	}

	ws, err := store.Get(t.Context(), "wsp_"+session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ws.ValidationOutcome != string(packagingdomain.OutcomePass) {
		t.Fatalf("expected workspace validation outcome PASS, got %q", ws.ValidationOutcome)
	}
	crBytes, err := os.ReadFile(filepath.Join(repoDir, "uds-package.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	checks := workspacepkg.Checks{}
	if reason := checks.PackageCR(string(crBytes)); reason != "" {
		t.Fatalf("PackageCR gate failed: %s", reason)
	}
}

func TestAppgenProfileSkillChangeUpdatesBoundProfileHash(t *testing.T) {
	svcA := planningapp.NewService(nil).WithAppgenSkillSource(appgenskill.Source)
	svcB := planningapp.NewService(nil).WithAppgenSkillSource(appgenskill.Source + "\n# additional convention\n")

	sessA, err := svcA.CreateWithInput(t.Context(), planningdomain.CreateInput{
		Profile:          planningdomain.ProfileAppgen,
		ProblemStatement: "Build a telemetry collector",
		AppName:          "telemetry-collector",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcA.Turn(t.Context(), sessA.ID, ""); err != nil {
		t.Fatal(err)
	}
	accA, err := svcA.Accept(t.Context(), sessA.ID)
	if err != nil {
		t.Fatal(err)
	}

	sessB, err := svcB.CreateWithInput(t.Context(), planningdomain.CreateInput{
		Profile:          planningdomain.ProfileAppgen,
		ProblemStatement: "Build a telemetry collector",
		AppName:          "telemetry-collector",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svcB.Turn(t.Context(), sessB.ID, ""); err != nil {
		t.Fatal(err)
	}
	accB, err := svcB.Accept(t.Context(), sessB.ID)
	if err != nil {
		t.Fatal(err)
	}

	if accA.Binding.Profile.Hash == "" || accB.Binding.Profile.Hash == "" {
		t.Fatalf("expected non-empty profile hashes: %q vs %q", accA.Binding.Profile.Hash, accB.Binding.Profile.Hash)
	}
	if accA.Binding.Profile.Hash == accB.Binding.Profile.Hash {
		t.Fatalf("changing appgen skill source did not change bound profile hash: %q", accA.Binding.Profile.Hash)
	}
}
