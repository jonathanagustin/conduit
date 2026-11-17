package composition

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	appgenpostgres "github.com/jonathanagustin/conduit/internal/appgen/adapters/postgres"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	appgenapp "github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func appgenHub(t *testing.T, store appgenapp.Store, sandbox appgenapp.Sandbox) http.Handler {
	t.Helper()
	handler, err := NewHub(
		controlplane.NewMemoryRepository(),
		controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true},
		Options{AppGenerator: recorded.Adapter{}, AppgenSandbox: sandbox, AppgenPublisher: &testAppgenPublisher{}, AppgenStore: store},
	)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

// generationsSurviveARestart creates a result on one Hub and reads it from a
// second Hub that shares only the store. The second Hub is what a restarted
// process, or another replica, is.
func generationsSurviveARestart(t *testing.T, store appgenapp.Store) {
	t.Helper()
	before := appgenHub(t, store, &testAppgenSandbox{})
	created := planningRequest(t, before, "ada", http.MethodPost, "/v1/app-generations", `{"name":"hello-http","description":"a private page","confirm":true}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create %d %s", created.Code, created.Body)
	}
	var result struct {
		ID           string `json:"id"`
		Owner        string `json:"owner"`
		RecordedDemo bool   `json:"recordedDemo"`
		Attempts     []struct {
			Number int `json:"number"`
		} `json:"attempts"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil || result.ID == "" || result.Owner != "ada" {
		t.Fatalf("result %s %v", created.Body, err)
	}
	// No model is configured, so the files are the checked-in demonstration.
	// The response says so, and the console can label it.
	if !result.RecordedDemo {
		t.Fatalf("the recorded demonstration is not marked: %s", created.Body)
	}
	if len(result.Attempts) != 1 || result.Attempts[0].Number != 1 {
		t.Fatalf("attempts = %+v", result.Attempts)
	}

	after := appgenHub(t, store, &testAppgenSandbox{})
	listed := planningRequest(t, after, "ada", http.MethodGet, "/v1/app-generations", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), result.ID) || !strings.Contains(listed.Body.String(), `"recordedDemo":true`) {
		t.Fatalf("ada list after restart %d %s", listed.Code, listed.Body)
	}
	read := planningRequest(t, after, "ada", http.MethodGet, "/v1/app-generations/"+result.ID, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), "a private page") {
		t.Fatalf("ada get after restart %d %s", read.Code, read.Body)
	}
	// The owner invariant holds for a result the second Hub did not create.
	other := planningRequest(t, after, "bea", http.MethodGet, "/v1/app-generations", "")
	if other.Code != http.StatusOK || strings.Contains(other.Body.String(), result.ID) || strings.Contains(other.Body.String(), "a private page") {
		t.Fatalf("bea list after restart %d %s", other.Code, other.Body)
	}
	hidden := planningRequest(t, after, "bea", http.MethodGet, "/v1/app-generations/"+result.ID, "")
	if hidden.Code != http.StatusNotFound || strings.Contains(hidden.Body.String(), "a private page") {
		t.Fatalf("bea get after restart %d %s", hidden.Code, hidden.Body)
	}
}

func TestAppGenerationsOutliveTheHubThatMadeThem(t *testing.T) {
	generationsSurviveARestart(t, &appgenapp.Memory{})
}

func TestPostgresAppGenerationsOutliveTheHubThatMadeThem(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
	}
	// Opened the way the executable opens it, and handed to the Hub.
	store, err := appgenpostgres.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	generationsSurviveARestart(t, store)
}

type absentBuildExecutor struct{ testAppgenSandbox }

func (*absentBuildExecutor) Available() error {
	return fmt.Errorf("%w: the go binary was not found on the Hub host", appgenapp.ErrBuildExecutorUnavailable)
}

func TestAppGenerationAnswers503WhenTheDeploymentCannotBuild(t *testing.T) {
	sandbox := &absentBuildExecutor{}
	handler := appgenHub(t, &appgenapp.Memory{}, sandbox)
	response := planningRequest(t, handler, "ada", http.MethodPost, "/v1/app-generations", `{"name":"hello-http","description":"a page","confirm":true}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d %s, want 503", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "build executor unavailable in this deployment") || strings.Contains(response.Body.String(), "exec:") {
		t.Fatalf("body = %s", response.Body)
	}
	if sandbox.builds != 0 {
		t.Fatalf("a build was attempted %d times", sandbox.builds)
	}
	if status := appGenerationBuildExecutor(sandbox); status.Available || !strings.Contains(status.Detail, "build executor unavailable") {
		t.Fatalf("status = %+v", status)
	}
	if status := appGenerationBuildExecutor(&testAppgenSandbox{}); !status.Available || status.Detail != "" {
		t.Fatalf("status of a working sandbox = %+v", status)
	}
}

func TestDefaultBuildExecutorIsUnavailableWithoutItsBinaries(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	status := AppGenerationBuildExecutor()
	if status.Available || !strings.Contains(status.Detail, "docker binary") {
		t.Fatalf("status = %+v, want unavailable and a named cause", status)
	}
}

func TestConfigureAppGenerationReadsAndRefusesSettings(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	base := newAppGenerationService(nil, recorded.Adapter{}, nil, &testAppgenSandbox{}, &testAppgenPublisher{}, env(nil))
	if base.RepairAttempts != appgenapp.DefaultRepairAttempts {
		t.Fatalf("default repair attempts = %d", base.RepairAttempts)
	}
	configured, err := configureAppGeneration(base, nil, &testAppgenSandbox{}, env(map[string]string{envAppgenRepairAttempts: "0", envAppgenBuildTimeout: "90s"}))
	if err != nil {
		t.Fatal(err)
	}
	if configured.RepairAttempts != 0 {
		t.Fatalf("repair attempts = %d, want 0", configured.RepairAttempts)
	}
	// The Hub opens no database itself. With no store supplied, results stay
	// in memory whatever the repository kind is.
	if _, ok := configured.Store.(*appgenapp.Memory); !ok {
		t.Fatalf("store with none supplied is %T, want memory", configured.Store)
	}
	explicit := &appgenapp.Memory{}
	if configured, err = configureAppGeneration(base, explicit, &testAppgenSandbox{}, env(map[string]string{"REPOSITORY_KIND": "postgres"})); err != nil || configured.Store != appgenapp.Store(explicit) {
		t.Fatalf("an explicit store was not used: %T %v", configured.Store, err)
	}
	for name, values := range map[string]map[string]string{
		"repairs not a number": {envAppgenRepairAttempts: "many"},
		"repairs negative":     {envAppgenRepairAttempts: "-1"},
		"repairs over the cap": {envAppgenRepairAttempts: "6"},
		"timeout not a time":   {envAppgenBuildTimeout: "soon"},
		"timeout not positive": {envAppgenBuildTimeout: "0s"},
	} {
		if _, err := configureAppGeneration(base, nil, &testAppgenSandbox{}, env(values)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
