package planner

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func TestTurnInspectsALocalFixtureAndRecordsAudit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script fixture is a shell")
	}
	fixture, sha := makeFixture(t)
	server := serveGit(t, fixture)
	script := writeModelScript(t, inspectReplies(askOne)...)
	audit := &memoryAudit{}
	planner := Planner{Argv: []string{script}, Timeout: 0, Audit: audit}
	service := application.NewService(planner)
	session, err := service.Create(context.Background(), server.URL+"/src.git", "main")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(context.Background(), session.ID, "please inspect")
	if err != nil {
		t.Fatal(err)
	}
	if session.ResolvedCommit != sha {
		t.Fatalf("resolved = %q, want %q", session.ResolvedCommit, sha)
	}
	if session.Status != domain.StatusNeedsInput || session.Plan != nil {
		t.Fatalf("status/plan = %s %+v", session.Status, session.Plan)
	}
	if session.Inspection == nil || session.Inspection.Language == "" {
		t.Fatalf("inspection = %+v", session.Inspection)
	}
	if strings.Join(session.Inspection.ToolCalls, ",") != "inspect_repo,read_file" {
		t.Fatalf("toolCalls = %#v", session.Inspection.ToolCalls)
	}
	var inspections int
	for _, event := range session.Events {
		if event.Type == domain.EventInspection {
			inspections++
		}
	}
	if inspections != 2 {
		t.Fatalf("inspection events = %d, events=%+v", inspections, session.Events)
	}
	if len(audit.rows) != 2 || audit.rows[0].PromptSHA256 == "" || audit.rows[0].ResponseSHA256 == "" {
		t.Fatalf("audit = %+v", audit.rows)
	}
	if strings.Join(audit.rows[0].ToolCalls, ",") != "inspect_repo,read_file" {
		t.Fatalf("audit tools = %#v", audit.rows[0].ToolCalls)
	}
	if audit.rows[0].ResolvedCommit != sha || audit.rows[0].Outcome != "pass" {
		t.Fatalf("audit record = %+v", audit.rows[0])
	}
}

func TestLiveExcalidrawInspect(t *testing.T) {
	if strings.TrimSpace(os.Getenv("CONDUIT_PLANNING_LIVE")) == "" || strings.TrimSpace(os.Getenv("CONDUIT_MODEL_CMD")) == "" {
		t.Skip("set CONDUIT_PLANNING_LIVE and CONDUIT_MODEL_CMD to run the live inspection")
	}
	argv, err := localcmd.ParseArgv(os.Getenv("CONDUIT_MODEL_CMD"))
	if err != nil {
		t.Fatal(err)
	}
	timeout := 5 * time.Minute
	if text := strings.TrimSpace(os.Getenv("CONDUIT_MODEL_TIMEOUT")); text != "" {
		parsed, err := time.ParseDuration(text)
		if err != nil || parsed <= 0 {
			t.Fatalf("CONDUIT_MODEL_TIMEOUT: %v", err)
		}
		timeout = parsed
	}
	audit := &memoryAudit{}
	var reads []string
	runner := Planner{Argv: argv, Timeout: timeout, Audit: audit, OnTool: func(name, path string) {
		if name == "read_file" && strings.TrimSpace(path) != "" {
			reads = append(reads, path)
		}
	}}
	service := application.NewService(runner)
	session, err := service.Create(context.Background(), "https://github.com/excalidraw/excalidraw", "3a317b55")
	if err != nil {
		t.Fatal(err)
	}
	session, err = service.Turn(context.Background(), session.ID, "inspect this repository")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(session.ResolvedCommit, "3a317b55") {
		t.Fatalf("resolved = %q", session.ResolvedCommit)
	}
	if session.Inspection == nil {
		t.Fatal("missing inspection")
	}
	joined := strings.Join(session.Inspection.ToolCalls, ",")
	if !strings.Contains(joined, "inspect_repo") {
		t.Fatalf("toolCalls = %#v", session.Inspection.ToolCalls)
	}
	if len(audit.rows) == 0 || audit.rows[0].PromptSHA256 == "" || audit.rows[0].ResponseSHA256 == "" {
		t.Fatalf("audit = %+v", audit.rows)
	}
	t.Logf("resolved commit %s", session.ResolvedCommit)
	t.Logf("tool calls %s", joined)
	t.Logf("files read %s", strings.Join(reads, ", "))
	t.Logf("events %d", len(session.Events))
}

func TestParseModelReplyExtractsFencedToolCalls(t *testing.T) {
	t.Parallel()
	raw := []byte("Here you go:\n```json\n{\"toolCalls\":[{\"name\":\"inspect_repo\",\"arguments\":{}}]}\n```\n")
	requests, isTools, err := parseModelReply(raw)
	if err != nil || !isTools || len(requests) != 1 || requests[0].Name != "inspect_repo" {
		t.Fatalf("got %#v %v %v", requests, isTools, err)
	}
}

type memoryAudit struct{ rows []Record }

func (audit *memoryAudit) Record(_ context.Context, record Record) error {
	audit.rows = append(audit.rows, record)
	return nil
}

// writeModelScript is a scripted fake model. Call n prints reply-n, and the
// last reply repeats. Each prompt is kept as prompt-n.txt beside the script.
func writeModelScript(t *testing.T, replies ...string) string {
	t.Helper()
	dir := t.TempDir()
	for i, reply := range replies {
		if err := os.WriteFile(filepath.Join(dir, "reply-"+strconv.Itoa(i+1)), []byte(reply+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "model")
	body := `#!/bin/sh
dir=$(dirname "$0")
n=0
if [ -f "$dir/n" ]; then n=$(cat "$dir/n"); fi
n=$((n+1))
echo "$n" > "$dir/n"
cat > "$dir/prompt-$n.txt"
total=` + strconv.Itoa(len(replies)) + `
if [ "$n" -gt "$total" ]; then n=$total; fi
cat "$dir/reply-$n"
`
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	toolInspect = `{"toolCalls":[{"name":"inspect_repo","arguments":{}}]}`
	toolRead    = `{"toolCalls":[{"name":"read_file","arguments":{"path":"README.md"}},{"name":"read_file","arguments":{"path":"package.json"}}]}`
	askOne      = `{"questions":[{"id":"node_version","prompt":"Which Node version?","choices":["18","20"]}]}`
	askNone     = `{"questions":[]}`
	planJSON    = `{"summary":"A small Node site.","baseImage":"node:20-slim","buildSteps":["npm ci","npm run build"],"ports":[8080,3000],"networkAllow":["registry.npmjs.org:443"],"sso":{"required":true,"notes":"operator login"},"risks":["no lockfile"]}`
)

// inspectReplies is the scripted exchange for the inspection turn: inspect,
// read, summary, then the questions phase (read, then final).
func inspectReplies(questions string) []string {
	return []string{
		toolInspect,
		`{"toolCalls":[{"name":"read_file","arguments":{"path":"README.md"}}]}`,
		"The fixture is a small JavaScript site with an npm start script.",
		toolRead,
		questions,
	}
}

func makeFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("fixture readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "package.json"), []byte(`{"name":"fixture","scripts":{"start":"node server.js"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	run("init", "--quiet", "-b", "main")
	run("config", "user.name", "fixture")
	run("config", "user.email", "fixture@local")
	run("add", ".")
	run("commit", "--quiet", "-m", "fixture")
	sha := strings.TrimSpace(run("rev-parse", "HEAD"))
	bare := filepath.Join(dir, "src.git")
	cmd := exec.Command("git", "clone", "--quiet", "--bare", src, bare)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bare clone: %v\n%s", err, out)
	}
	if err := exec.Command("git", "--git-dir", bare, "update-server-info").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "--git-dir", bare, "config", "http.uploadpack", "true").Run(); err != nil {
		t.Fatal(err)
	}
	return dir, sha
}

func serveGit(t *testing.T, projectRoot string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body bytes.Buffer
		_, _ = io.Copy(&body, request.Body)
		cmd := exec.Command("git", "http-backend")
		cmd.Env = append(os.Environ(),
			"GIT_PROJECT_ROOT="+projectRoot,
			"GIT_HTTP_EXPORT_ALL=1",
			"PATH_INFO="+request.URL.Path,
			"PATH_TRANSLATED="+filepath.Join(projectRoot, request.URL.Path),
			"QUERY_STRING="+request.URL.RawQuery,
			"REQUEST_METHOD="+request.Method,
			"CONTENT_TYPE="+request.Header.Get("Content-Type"),
			"CONTENT_LENGTH="+strconv.Itoa(body.Len()),
		)
		cmd.Stdin = &body
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			http.Error(writer, stderr.String(), http.StatusInternalServerError)
			return
		}
		out := stdout.Bytes()
		header, rest, found := bytes.Cut(out, []byte("\r\n\r\n"))
		if !found {
			header, rest, found = bytes.Cut(out, []byte("\n\n"))
		}
		if !found {
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(out)
			return
		}
		status := http.StatusOK
		for _, line := range strings.Split(string(header), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if strings.EqualFold(key, "Status") {
				if code, _, _ := strings.Cut(value, " "); code != "" {
					if parsed, err := strconv.Atoi(code); err == nil {
						status = parsed
					}
				}
				continue
			}
			writer.Header().Add(key, value)
		}
		writer.WriteHeader(status)
		_, _ = writer.Write(rest)
	}))
	t.Cleanup(server.Close)
	return server
}
