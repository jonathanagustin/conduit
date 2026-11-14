// Package sandbox builds and starts one generated image with the local
// container engine. The build has no network. The process is not root.
package sandbox

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/application"
)

// Docker is the local container engine.
type Docker struct{}

func (Docker) Build(ctx context.Context, input application.BuildRequest) (application.BuildResult, error) {
	dir, err := os.MkdirTemp("", "conduit-appgen-")
	if err != nil {
		return application.BuildResult{}, err
	}
	defer os.RemoveAll(dir)
	for path, body := range input.Sources {
		if err := writeFile(dir, path, body); err != nil {
			return fail(err.Error()), nil
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), input.Dockerfile, 0o644); err != nil {
		return application.BuildResult{}, err
	}
	goMod := "module example.com/" + input.Spec.Name + "\n\ngo 1.26.0\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return application.BuildResult{}, err
	}
	build := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-trimpath", "-ldflags=-s -w", "-o", "app", ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOPROXY=off", "GOTOOLCHAIN=local")
	if output, err := build.CombinedOutput(); err != nil {
		return fail("compile failed: " + firstLine(output)), nil
	}
	if err := os.Chmod(filepath.Join(dir, "app"), 0o755); err != nil {
		return application.BuildResult{}, err
	}
	tag := "appgen-" + input.Spec.Name + ":local"
	output, err := exec.CommandContext(ctx, "docker", "build", "--network", "none", "--tag", tag, dir).CombinedOutput()
	if err != nil {
		return fail("image build failed: " + firstLine(output)), nil
	}
	id, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", tag).Output()
	if err != nil {
		return fail("image id was not recorded"), nil
	}
	return application.BuildResult{Passed: true, Reason: "image built with no network as a non-root scratch image", ImageID: strings.TrimSpace(string(id))}, nil
}

func (Docker) Check(ctx context.Context, built application.BuildResult, input application.BuildRequest) (application.HealthResult, error) {
	if strings.TrimSpace(built.ImageID) == "" {
		return application.HealthResult{Reason: "no image to start"}, nil
	}
	tag := "appgen-" + input.Spec.Name + ":local"
	name := "appgen-check-" + input.Spec.Name
	_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()
	defer exec.Command("docker", "rm", "-f", name).Run()
	memory := memoryFlag(input.Spec.Resources.Memory)
	base := []string{
		"run", "--detach", "--name", name, "--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges:true", "--user", "65532:65532",
		"--memory", memory, "--pids-limit", "64",
	}
	port := input.Spec.Port
	args := append(append([]string{}, base...), "--network", "bridge", "-p", fmt.Sprintf("127.0.0.1::%d", port), tag)
	if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		if !bridgeUnavailable(string(output)) {
			return application.HealthResult{Reason: "container did not start: " + firstLine(output)}, nil
		}
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", name).Run()
		hostPort, err := freePort()
		if err != nil {
			return application.HealthResult{Reason: "no local port for the health check"}, nil
		}
		port = hostPort
		args = append(append([]string{}, base...), "--network", "host", "-e", "PORT="+strconv.Itoa(port), tag)
		if output, err = exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
			return application.HealthResult{Reason: "container did not start on the host network: " + firstLine(output)}, nil
		}
		if reason := waitHealth(ctx, port); reason != "" {
			return application.HealthResult{Reason: reason}, nil
		}
		return application.HealthResult{Passed: true, Reason: "GET /health returned 200 on the host network"}, nil
	}
	mapped, err := exec.CommandContext(ctx, "docker", "port", name, strconv.Itoa(port)+"/tcp").Output()
	if err != nil {
		return application.HealthResult{Reason: "published port was not recorded"}, nil
	}
	hostPort, ok := hostPortOf(string(mapped))
	if !ok {
		return application.HealthResult{Reason: "published port was not a host address"}, nil
	}
	if reason := waitHealth(ctx, hostPort); reason != "" {
		logs, _ := exec.CommandContext(ctx, "docker", "logs", name).CombinedOutput()
		if text := firstLine(logs); text != "" {
			reason = reason + ": " + text
		}
		return application.HealthResult{Reason: reason}, nil
	}
	return application.HealthResult{Passed: true, Reason: "GET /health returned 200 within the timeout"}, nil
}

func waitHealth(ctx context.Context, port int) string {
	deadline := time.Now().Add(8 * time.Second)
	client := http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	var last string
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return "health check cancelled"
		}
		response, err := client.Get(url)
		if err != nil {
			last = err.Error()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body := make([]byte, 64)
		n, _ := response.Body.Read(body)
		response.Body.Close()
		if response.StatusCode == http.StatusOK && strings.TrimSpace(string(body[:n])) == "ok" {
			return ""
		}
		last = fmt.Sprintf("status %d", response.StatusCode)
		time.Sleep(200 * time.Millisecond)
	}
	if last == "" {
		last = "no response"
	}
	return "health check timed out: " + last
}

func bridgeUnavailable(output string) bool {
	text := strings.ToLower(output)
	return strings.Contains(text, "bridge") || strings.Contains(text, "iptables") || strings.Contains(text, "network") && strings.Contains(text, "not found")
}

func hostPortOf(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	line := strings.Split(value, "\n")[0]
	_, port, err := net.SplitHostPort(line)
	if err != nil {
		return 0, false
	}
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 {
		return 0, false
	}
	return parsed, true
}

func memoryFlag(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "Mi") {
		return strings.TrimSuffix(value, "Mi") + "m"
	}
	if strings.HasSuffix(value, "Gi") {
		number := strings.TrimSuffix(value, "Gi")
		n, err := strconv.Atoi(number)
		if err != nil {
			return "64m"
		}
		return strconv.Itoa(n*1024) + "m"
	}
	return "64m"
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func writeFile(dir, name string, body []byte) error {
	if name != filepath.Base(name) || strings.Contains(name, "..") {
		return fmt.Errorf("source path %s is not a file name", name)
	}
	return os.WriteFile(filepath.Join(dir, name), body, 0o644)
}

func firstLine(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "no output"
	}
	line, _, _ := strings.Cut(text, "\n")
	line = strings.TrimSpace(line)
	if len(line) > 240 {
		line = line[:240]
	}
	return line
}

func fail(reason string) application.BuildResult {
	return application.BuildResult{Reason: reason}
}
