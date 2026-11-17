package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/application"
)

// fakeTools puts a fake compiler and engine first on PATH. Each records its
// environment and working directory beside itself, because the child gets an
// explicit environment and cannot be told where to write through a variable.
func fakeTools(t *testing.T, goScript string) string {
	t.Helper()
	dir := t.TempDir()
	image := "sha256:" + strings.Repeat("c", 64)
	if goScript == "" {
		goScript = `#!/bin/sh
d=$(dirname "$0")
env > "$d/go-env"
pwd >> "$d/go-dirs"
printf compiled > app
`
	}
	docker := `#!/bin/sh
d=$(dirname "$0")
env > "$d/docker-env"
case "$1" in
run) echo owned-container; exit 0 ;;
image) env > "$d/docker-image-env"; exit 0 ;;
port) exit 1 ;;
inspect) echo '{}'; exit 0 ;;
rm) exit 0 ;;
esac
while [ "$#" -gt 0 ]; do
 if [ "$1" = --iidfile ]; then shift; printf '%s\n' "` + image + `" > "$1"; exit 0; fi
 shift
done
exit 1
`
	for name, script := range map[string]string{"go": goScript, "docker": docker} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func buildInput() application.BuildRequest {
	return application.BuildRequest{Sources: map[string][]byte{"main.go": []byte("package main")}}
}

func TestBuildDoesNotInheritTheHubEnvironment(t *testing.T) {
	dir := fakeTools(t, "")
	for name, value := range map[string]string{
		"POSTGRES_DSN":          "postgres://hub:hunter2-dsn@db/conduit",
		"OIDC_CLIENT_SECRET":    "hunter2-oidc",
		"ANTHROPIC_API_KEY":     "hunter2-model",
		"CONDUIT_SIGNING_KEY":   "hunter2-signing",
		"GOFLAGS":               "-mod=mod",
		"GOPROXY":               "https://proxy.invalid",
		"DOCKER_HOST":           "unix:///run/fixture.sock",
		"CONDUIT_MODEL_API_KEY": "hunter2-http-model",
	} {
		t.Setenv(name, value)
	}
	built, err := (Docker{}).Build(context.Background(), buildInput())
	if err != nil || !built.Passed {
		t.Fatalf("build = %+v %v", built, err)
	}
	// The health check starts the engine too. It must get the same environment.
	if _, err := (Docker{}).Check(context.Background(), built, buildInput()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"go-env", "docker-env"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		seen := string(raw)
		for _, leaked := range []string{"hunter2", "POSTGRES_DSN", "OIDC_CLIENT_SECRET", "ANTHROPIC_API_KEY", "CONDUIT_", "proxy.invalid", "-mod=mod"} {
			if strings.Contains(seen, leaked) {
				t.Errorf("%s: the child saw %q from the Hub environment", name, leaked)
			}
		}
		for _, want := range []string{"GOPROXY=off\n", "GOFLAGS=\n", "GOENV=off\n", "GOTOOLCHAIN=local\n", "CGO_ENABLED=0\n", "PATH=", "DOCKER_HOST=unix:///run/fixture.sock\n"} {
			if !strings.Contains(seen, want) {
				t.Errorf("%s: the child environment lacks %q", name, strings.TrimSpace(want))
			}
		}
	}
}

func TestBuildEnvKeepsOnlyTheAllowlistAndFixesTheToolchain(t *testing.T) {
	env := BuildEnv([]string{
		"PATH=/usr/bin", "HOME=/home/hub", "LC_ALL=C", "DOCKER_HOST=tcp://engine:2376",
		"POSTGRES_DSN_FILE=/secrets/dsn", "OPENAI_API_KEY=k", "GOFLAGS=-toolexec=/tmp/x", "GOPROXY=direct", "=odd", "NOVALUE",
	})
	joined := "\n" + strings.Join(env, "\n") + "\n"
	for _, want := range []string{"\nPATH=/usr/bin\n", "\nHOME=/home/hub\n", "\nLC_ALL=C\n", "\nDOCKER_HOST=tcp://engine:2376\n", "\nGOPROXY=off\n", "\nGOFLAGS=\n"} {
		if !strings.Contains(joined, want) {
			t.Errorf("environment lacks %q: %q", strings.TrimSpace(want), env)
		}
	}
	for _, refused := range []string{"POSTGRES", "OPENAI", "toolexec", "GOPROXY=direct", "NOVALUE", "odd"} {
		if strings.Contains(joined, refused) {
			t.Errorf("environment carries %q: %q", refused, env)
		}
	}
}

func TestBuildUsesAFreshDirectoryAndRemovesIt(t *testing.T) {
	dir := fakeTools(t, "")
	for range 2 {
		if built, err := (Docker{}).Build(context.Background(), buildInput()); err != nil || !built.Passed {
			t.Fatalf("build = %+v %v", built, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "go-dirs"))
	if err != nil {
		t.Fatal(err)
	}
	dirs := strings.Fields(string(raw))
	if len(dirs) != 2 || dirs[0] == dirs[1] {
		t.Fatalf("build directories = %q, want two different ones", dirs)
	}
	here, _ := os.Getwd()
	for _, used := range dirs {
		if used == here || strings.HasPrefix(here, used) {
			t.Fatalf("build ran in the Hub's working directory %s", used)
		}
		if _, err := os.Stat(used); !os.IsNotExist(err) {
			t.Fatalf("build directory %s was left behind: %v", used, err)
		}
	}
}

func TestBuildStopsAtTheTimeLimit(t *testing.T) {
	fakeTools(t, "#!/bin/sh\nexec sleep 30\n")
	started := time.Now()
	built, err := (Docker{BuildTimeout: 300 * time.Millisecond}).Build(context.Background(), buildInput())
	if err != nil {
		t.Fatal(err)
	}
	if built.Passed || !strings.Contains(built.Reason, "time limit") {
		t.Fatalf("build = %+v, want a time-limit failure", built)
	}
	if elapsed := time.Since(started); elapsed > 15*time.Second {
		t.Fatalf("build took %s to stop at a 300ms limit", elapsed)
	}
}

func TestBuildOutputIsCapped(t *testing.T) {
	fakeTools(t, "#!/bin/sh\ni=0\nwhile [ $i -lt 4000 ]; do printf '%0512d\\n' 7; i=$((i+1)); done\nexit 1\n")
	built, err := (Docker{}).Build(context.Background(), buildInput())
	if err != nil {
		t.Fatal(err)
	}
	if built.Passed || !strings.HasPrefix(built.Reason, "compile failed: ") {
		t.Fatalf("build = %+v", built.Reason)
	}
	if len(built.Reason) > 300 {
		t.Fatalf("gate reason is %d bytes", len(built.Reason))
	}
	if len(built.Output) == 0 || len(built.Output) > feedbackBytes+64 || !strings.HasSuffix(built.Output, "[output truncated]") {
		t.Fatalf("failure output is %d bytes, want at most %d and marked truncated", len(built.Output), feedbackBytes+64)
	}
	writer := &capped{limit: 8}
	for range 1000 {
		_, _ = writer.Write([]byte("0123456789"))
	}
	if got := string(writer.Bytes()); got != "01234567\n[output truncated]" {
		t.Fatalf("capped writer kept %q", got)
	}
}

func TestMissingBinariesAreReportedAsAnUnavailableExecutor(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := (Docker{}).Available()
	if !errors.Is(err, application.ErrBuildExecutorUnavailable) {
		t.Fatalf("Available = %v, want ErrBuildExecutorUnavailable", err)
	}
	if !strings.Contains(err.Error(), "build executor unavailable in this deployment") || !strings.Contains(err.Error(), "docker binary") {
		t.Fatalf("message = %q", err)
	}
	built, err := (Docker{}).Build(context.Background(), buildInput())
	if !errors.Is(err, application.ErrBuildExecutorUnavailable) {
		t.Fatalf("Build = %+v %v, want ErrBuildExecutorUnavailable and not an exec failure as a gate reason", built, err)
	}
	if strings.Contains(err.Error(), "exec:") || strings.Contains(err.Error(), "$PATH") {
		t.Fatalf("message repeats the exec failure: %q", err)
	}
}

// The compiler is needed for main.go and index.html only. A host with the
// engine and no compiler can still build main.py, so it is available, and a
// reply that needs the compiler is told the executor cannot build it.
func TestAHostWithoutTheCompilerBuildsPythonAndRefusesGo(t *testing.T) {
	dir := fakeTools(t, "")
	if err := os.Remove(filepath.Join(dir, "go")); err != nil {
		t.Fatal(err)
	}
	// The fake engine is a shell script and needs the system tools.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	if found, err := exec.LookPath("go"); err == nil {
		t.Skipf("a compiler is installed among the system tools at %s", found)
	}
	t.Setenv("OIDC_CLIENT_SECRET", "hunter2-oidc")
	if err := (Docker{}).Available(); err != nil {
		t.Fatalf("Available = %v, want nil with the engine present", err)
	}
	python := application.BuildRequest{Sources: map[string][]byte{"main.py": []byte("print('ok')\n")}}
	built, err := (Docker{}).Build(context.Background(), python)
	if err != nil || !built.Passed {
		t.Fatalf("python build = %+v %v", built, err)
	}
	// The runtime image lookup is an engine child too and gets the same environment.
	raw, err := os.ReadFile(filepath.Join(dir, "docker-image-env"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") || !strings.Contains(string(raw), "GOPROXY=off\n") {
		t.Fatalf("the runtime image lookup did not get the build environment:\n%s", raw)
	}
	_, err = (Docker{}).Build(context.Background(), buildInput())
	if !errors.Is(err, application.ErrBuildExecutorUnavailable) || !strings.Contains(err.Error(), "go binary") {
		t.Fatalf("go build with no compiler: err = %v, want ErrBuildExecutorUnavailable naming go", err)
	}
}

func TestAvailableWithBothBinaries(t *testing.T) {
	fakeTools(t, "")
	if err := (Docker{}).Available(); err != nil {
		t.Fatal(err)
	}
}
