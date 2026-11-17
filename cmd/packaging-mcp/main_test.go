package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// helperEnv makes the test binary behave as packaging-mcp, so the tests talk
// to a real child process over its real standard input and output.
const helperEnv = "PACKAGING_MCP_TEST_SERVE"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		args := []string{}
		if raw := os.Getenv(helperEnv + "_ARGS"); raw != "" {
			_ = json.Unmarshal([]byte(raw), &args)
		}
		os.Args = append([]string{"packaging-mcp"}, args...)
		main()
		return
	}
	os.Exit(m.Run())
}

// stdioClient is a hand-written MCP client for the stdio transport:
// newline-delimited JSON-RPC, one message per line.
type stdioClient struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan []byte
	stderr *stderrBuffer
	nextID int
}

// os/exec writes stderr from a copying goroutine while a failing assertion may
// need a snapshot before the child exits. Do not expose bytes.Buffer.ReadFrom:
// io.Copy must go through the synchronized Write method too.
type stderrBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *stderrBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(data)
}

func (buffer *stderrBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

func start(t *testing.T, args ...string) *stdioClient {
	t.Helper()
	encoded, _ := json.Marshal(args)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"=1", helperEnv+"_ARGS="+string(encoded))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	client := &stdioClient{t: t, cmd: cmd, stdin: stdin, lines: make(chan []byte, 16), stderr: &stderrBuffer{}}
	cmd.Stderr = client.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(client.lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)
		for scanner.Scan() {
			client.lines <- append([]byte(nil), scanner.Bytes()...)
		}
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return client
}

func (client *stdioClient) write(message map[string]any) {
	client.t.Helper()
	message["jsonrpc"] = "2.0"
	raw, _ := json.Marshal(message)
	if _, err := client.stdin.Write(append(raw, '\n')); err != nil {
		client.t.Fatalf("write: %v\nstderr: %s", err, client.stderr.String())
	}
}

type reply struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (client *stdioClient) request(method string, params any) reply {
	client.t.Helper()
	client.nextID++
	client.write(map[string]any{"id": client.nextID, "method": method, "params": params})
	for {
		select {
		case line, open := <-client.lines:
			if !open {
				client.t.Fatalf("the server closed stdout before answering %s\nstderr: %s", method, client.stderr.String())
			}
			var got reply
			if err := json.Unmarshal(line, &got); err != nil {
				client.t.Fatalf("stdout carried a line that is not JSON-RPC: %q", line)
			}
			if got.ID == client.nextID {
				return got
			}
		case <-time.After(20 * time.Second):
			client.t.Fatalf("no answer to %s\nstderr: %s", method, client.stderr.String())
		}
	}
}

func (client *stdioClient) handshake() {
	client.t.Helper()
	got := client.request("initialize", map[string]any{
		"protocolVersion": "2025-11-25", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "packaging-mcp-test-client", "version": "1.0.0"},
	})
	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(got.Result, &result); err != nil || got.Error != nil || result.ProtocolVersion != "2025-11-25" || result.ServerInfo.Name != "packaging-tools" {
		client.t.Fatalf("initialize = %s %+v", got.Result, got.Error)
	}
	client.write(map[string]any{"method": "notifications/initialized"})
}

func (client *stdioClient) tools() []string {
	client.t.Helper()
	got := client.request("tools/list", map[string]any{})
	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(got.Result, &listed); err != nil || got.Error != nil {
		client.t.Fatalf("tools/list = %s %+v", got.Result, got.Error)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func (client *stdioClient) call(name string, arguments any) (reply, string, bool) {
	client.t.Helper()
	got := client.request("tools/call", map[string]any{"name": name, "arguments": arguments})
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if got.Error != nil {
		return got, "", true
	}
	if err := json.Unmarshal(got.Result, &result); err != nil || len(result.Content) == 0 {
		client.t.Fatalf("tools/call %s = %s", name, got.Result)
	}
	return got, result.Content[0].Text, result.IsError
}

func repository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range map[string]string{
		"README.md": "# site\n", "go.mod": "module example.invalid/site\n\ngo 1.22\n", ".env": "TOKEN=not-a-real-secret\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestWithARepositoryRootTheRepositoryToolsWorkOverStdio(t *testing.T) {
	root := repository(t)
	for _, args := range [][]string{{"--repo", root}, {root}} {
		client := start(t, args...)
		client.handshake()
		want := []string{"dry_run_gates", "inspect_image", "inspect_repo", "read_file", "validate_dockerfile", "validate_manifests"}
		if got := client.tools(); !reflect.DeepEqual(got, want) {
			t.Fatalf("args %v: tools/list = %v, want %v", args, got, want)
		}
		_, inspected, failed := client.call("inspect_repo", map[string]any{})
		if failed || !strings.Contains(inspected, `"language":"go"`) || !strings.Contains(inspected, "README.md") {
			t.Fatalf("args %v: inspect_repo = %s", args, inspected)
		}
		_, read, failed := client.call("read_file", map[string]any{"path": "README.md"})
		if failed || !strings.Contains(read, "# site") {
			t.Fatalf("args %v: read_file = %s", args, read)
		}
		// The confinement and the secret-path denial are the toolbox's own.
		for _, path := range []string{"../outside", ".env"} {
			if _, text, failed := client.call("read_file", map[string]any{"path": path}); !failed || strings.Contains(text, "not-a-real-secret") {
				t.Fatalf("args %v: read_file %s was served: %s", args, path, text)
			}
		}
	}
}

func TestWithoutARepositoryRootTheRepositoryToolsAreAbsent(t *testing.T) {
	client := start(t)
	client.handshake()
	want := []string{"dry_run_gates", "inspect_image", "validate_dockerfile", "validate_manifests"}
	if got := client.tools(); !reflect.DeepEqual(got, want) {
		t.Fatalf("tools/list = %v, want %v", got, want)
	}
	for _, name := range []string{"inspect_repo", "read_file"} {
		got, text, _ := client.call(name, map[string]any{"path": "README.md"})
		if got.Error == nil {
			t.Fatalf("%s is callable with no repository bound: %s", name, text)
		}
		if strings.Contains(got.Error.Message, "no repository is bound") {
			t.Fatalf("%s is present and failing rather than absent: %s", name, got.Error.Message)
		}
	}
	// A tool that needs no repository still works.
	if _, text, failed := client.call("validate_dockerfile", map[string]any{"dockerfile": "FROM scratch\n"}); failed && text == "" {
		t.Fatal("validate_dockerfile gave no result")
	}
	if !strings.Contains(client.stderr.String(), "no repository root given") {
		t.Fatalf("the server did not say on stderr that the repository tools are off: %q", client.stderr.String())
	}
}

func TestARootThatIsNotADirectoryStopsTheServer(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--repo", file}, {filepath.Join(t.TempDir(), "missing")}, {"a", "b"}, {"--repo", t.TempDir(), t.TempDir()}} {
		var stderr bytes.Buffer
		if status := run(t.Context(), args, &stderr, nil); status != 2 || stderr.Len() == 0 {
			t.Fatalf("args %v: status %d, stderr %q", args, status, stderr.String())
		}
	}
}
