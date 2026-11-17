package composition

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/devserver"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	planningworkflow "github.com/jonathanagustin/conduit/internal/planning/workflow"
)

func TestPlanningReplicasRefuseProcessLocalStore(t *testing.T) {
	err := ValidatePlanningReplicas(func(name string) string {
		if name == "HUB_REPLICA_COUNT" {
			return "2"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "HUB_REPLICA_COUNT=2") || !strings.Contains(err.Error(), "process-local") {
		t.Fatalf("expected a process-local refusal, got %v", err)
	}
	if err := ValidatePlanningReplicas(func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlanningReplicas(func(name string) string {
		if name == "HUB_REPLICA_COUNT" {
			return "1"
		}
		return ""
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMountPlanningAllowsSharedFrontendAndRefusesHostnameWithoutIt(t *testing.T) {
	t.Setenv("CONDUIT_TEMPORAL_ADDRESS", "planning-frontend.platform-system.svc:7233")
	t.Setenv("CONDUIT_TEMPORAL_SHARED", "")
	_, err := mountPlanning(http.NotFoundHandler(), controlplane.NewMemoryRepository(), controlplane.HandlerOptions{})
	if err == nil || !strings.Contains(err.Error(), "refusing non-loopback") {
		t.Fatalf("hostname without shared flag: %v", err)
	}
	t.Setenv("HUB_REPLICA_COUNT", "2")
	t.Setenv("CONDUIT_TEMPORAL_SHARED", "true")
	handler, err := mountPlanning(http.NotFoundHandler(), controlplane.NewMemoryRepository(), controlplane.HandlerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if handler == nil {
		t.Fatal("missing handler")
	}
}

func TestPlanningSessionIsVisibleToTheOtherReplica(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	workflowClient := newFakeTemporalWorkflowClient()
	options := controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}
	first := planningHandlerWithTemporal(http.NotFoundHandler(), repository, options, nil, workflowClient)
	second := planningHandlerWithTemporal(http.NotFoundHandler(), repository, options, nil, workflowClient)
	created := createPlanningSession(t, first)
	loaded := planningRequest(t, second, "alice", http.MethodGet, "/v1/planning-sessions/"+created.ID, "")
	var got planningdomain.Session
	if loaded.Code != http.StatusOK || json.Unmarshal(loaded.Body.Bytes(), &got) != nil || got.ID != created.ID || got.Intake.RepoURL != created.Intake.RepoURL {
		t.Fatalf("other replica: %d %s", loaded.Code, loaded.Body.String())
	}
	restarted := planningHandlerWithTemporal(http.NotFoundHandler(), controlplane.NewMemoryRepository(), options, nil, workflowClient)
	again := planningRequest(t, restarted, "alice", http.MethodGet, "/v1/planning-sessions/"+created.ID, "")
	if again.Code != http.StatusOK || json.Unmarshal(again.Body.Bytes(), &got) != nil || got.ID != created.ID {
		t.Fatalf("restarted replica: %d %s", again.Code, again.Body.String())
	}
}

func TestPlanningSessionSurvivesWorkflowFrontendRestart(t *testing.T) {
	cli := planningWorkflowCLI(t)
	database := filepath.Join(t.TempDir(), "planning.db")
	start := func() (string, *devserver.Server) {
		t.Helper()
		running, err := devserver.Start(t.Context(), devserver.Options{CLIPath: cli, DBPath: database})
		if err != nil {
			t.Fatal(err)
		}
		return running.Frontend, running.Server
	}
	frontend, server := start()
	repository := controlplane.NewMemoryRepository()
	options := controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}
	first, stopFirst := planningReplica(t, frontend, repository, options)
	second, stopSecond := planningReplica(t, frontend, repository, options)
	created := createPlanningSession(t, first)
	loaded := planningRequest(t, second, "alice", http.MethodGet, "/v1/planning-sessions/"+created.ID, "")
	if loaded.Code != http.StatusOK {
		t.Fatalf("other replica before restart: %d %s", loaded.Code, loaded.Body.String())
	}
	stopFirst()
	stopSecond()
	if err := server.Stop(); err != nil {
		t.Fatal(err)
	}
	frontend, server = start()
	defer server.Stop()
	restarted, stopRestarted := planningReplica(t, frontend, repository, options)
	defer stopRestarted()
	deadline := time.Now().Add(20 * time.Second)
	var again planningdomain.Session
	for {
		response := planningRequest(t, restarted, "alice", http.MethodGet, "/v1/planning-sessions/"+created.ID, "")
		if response.Code == http.StatusOK && json.Unmarshal(response.Body.Bytes(), &again) == nil && again.ID == created.ID && again.Intake.Ref == "main" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %s did not survive frontend restart: %d %s", created.ID, response.Code, response.Body.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func createPlanningSession(t *testing.T, handler http.Handler) planningdomain.Session {
	t.Helper()
	response := planningRequest(t, handler, "alice", http.MethodPost, "/v1/planning-sessions", `{"repoUrl":"https://github.com/org/app","ref":"main"}`)
	var created planningdomain.Session
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &created) != nil || created.ID == "" {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	return created
}

func planningReplica(t *testing.T, frontend string, repository controlplane.Repository, options controlplane.HandlerOptions) (http.Handler, func()) {
	t.Helper()
	workflowClient, err := client.Dial(client.Options{HostPort: frontend, Namespace: "default"})
	if err != nil {
		t.Fatal(err)
	}
	work := planningworkflow.RegisterWorker(workflowClient, planningworkflow.TaskQueue, planningworkflow.Activities{
		Auditor: workflowAcceptAuditor{repository: repository},
	})
	if err := work.Start(); err != nil {
		workflowClient.Close()
		t.Fatal(err)
	}
	handler := planningHandlerWithTemporal(http.NotFoundHandler(), repository, options, nil, workflowClient)
	return handler, func() {
		work.Stop()
		workflowClient.Close()
	}
}

func planningWorkflowCLI(t *testing.T) string {
	t.Helper()
	candidates := []string{os.Getenv("CONDUIT_TEMPORAL_CLI")}
	root := filepath.Join("..", "..", "..", ".cache", "temporal", "temporal")
	candidates = append(candidates, root)
	for _, path := range candidates {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	t.Skip("workflow CLI is not installed")
	return ""
}
