// Package sandbox builds and starts one generated image with the local
// container engine. The build has no network. The process is not root.
package sandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/appgen/domain"
)

// Docker is the local container engine. Every child process it starts gets
// the explicit environment BuildEnv returns and never the Hub's own.
type Docker struct {
	// BuildTimeout bounds the compile and the image build together. Zero
	// means DefaultBuildTimeout.
	BuildTimeout time.Duration
}

// Build compiles the sources, when the shape has any to compile, and builds
// the image in a new temporary directory that is removed before it returns.
func (docker Docker) Build(parent context.Context, input application.BuildRequest) (application.BuildResult, error) {
	shape, err := buildShape(input)
	if err != nil {
		return fail(err.Error()), nil
	}
	timeout := docker.BuildTimeout
	if timeout <= 0 {
		timeout = DefaultBuildTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
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
	reason := "image built with no network as a non-root scratch image"
	switch shape {
	case domain.ShapeGo, domain.ShapeHTML:
		if shape == domain.ShapeHTML {
			if err := os.WriteFile(filepath.Join(dir, "static.go"), []byte(staticServerSource), 0o644); err != nil {
				return application.BuildResult{}, err
			}
		}
		goMod := "module example.com/" + input.Spec.Name + "\n\ngo 1.26.0\n"
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
			return application.BuildResult{}, err
		}
		if output, err := run(ctx, dir, "go", "build", "-buildvcs=false", "-trimpath", "-ldflags=-s -w", "-o", "app", "."); err != nil {
			if missing := unavailable("go", err); missing != nil {
				return application.BuildResult{}, missing
			}
			if ctx.Err() != nil && parent.Err() == nil {
				return fail("compile exceeded the build time limit of " + timeout.String()), nil
			}
			return application.BuildResult{Reason: "compile failed: " + firstLine(output), Output: feedback(output)}, nil
		}
		if err := os.Chmod(filepath.Join(dir, "app"), 0o755); err != nil {
			return application.BuildResult{}, err
		}
	case domain.ShapePython:
		reason = "image built with no network as a non-root interpreter image"
		if err := ensureImage(ctx, domain.PythonRuntimeImage); err != nil {
			if errors.Is(err, application.ErrBuildExecutorUnavailable) {
				return application.BuildResult{}, err
			}
			return fail("python runtime image is not available: " + err.Error()), nil
		}
	default:
		return fail(domain.AllowedSourceShapes), nil
	}
	tag := "appgen-build-" + filepath.Base(dir) + ":local"
	output, err := run(ctx, dir, "docker", "build", "--network", "none", "--tag", tag, "--iidfile", filepath.Join(dir, "image-id"), dir)
	if err != nil {
		if missing := unavailable("docker", err); missing != nil {
			return application.BuildResult{}, missing
		}
		if ctx.Err() != nil && parent.Err() == nil {
			return fail("image build exceeded the build time limit of " + timeout.String()), nil
		}
		return application.BuildResult{Reason: "image build failed: " + firstLine(output), Output: feedback(output)}, nil
	}
	id, err := os.ReadFile(filepath.Join(dir, "image-id"))
	if err != nil || !imageIDPattern.MatchString(strings.TrimSpace(string(id))) {
		return fail("image id was not recorded"), nil
	}
	return application.BuildResult{Passed: true, Reason: reason, ImageID: strings.TrimSpace(string(id))}, nil
}

func buildShape(input application.BuildRequest) (domain.Shape, error) {
	if len(input.Spec.Files) > 0 {
		shape, reason, ok := domain.ShapeOf(input.Spec)
		if !ok {
			return "", fmt.Errorf("%s", reason)
		}
		return shape, nil
	}
	names := make([]string, 0, len(input.Sources))
	for name := range input.Sources {
		names = append(names, name)
	}
	sort.Strings(names)
	shape, reason, ok := domain.ShapeFromPaths(names)
	if !ok {
		return "", fmt.Errorf("%s", reason)
	}
	return shape, nil
}

func ensureImage(ctx context.Context, ref string) error {
	_, err := run(ctx, "", "docker", "image", "inspect", ref)
	if err == nil {
		return nil
	}
	if missing := unavailable("docker", err); missing != nil {
		return missing
	}
	output, err := run(ctx, "", "docker", "pull", ref)
	if err != nil {
		return fmt.Errorf("%s", firstLine(output))
	}
	return nil
}

var imageIDPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func (Docker) Check(ctx context.Context, built application.BuildResult, input application.BuildRequest) (application.HealthResult, error) {
	if !built.Passed || !imageIDPattern.MatchString(built.ImageID) {
		return application.HealthResult{Reason: "no verified image to start"}, nil
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return application.HealthResult{}, err
	}
	name := "appgen-check-" + hex.EncodeToString(nonce[:])
	// A failed start can still create a container. Cleanup is restricted to
	// this invocation's unpredictable name; never remove a shared app name.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = run(cleanup, "", "docker", "rm", "-f", name)
	}()
	args := []string{
		"run", "--detach", "--name", name, "--read-only", "--tmpfs", "/tmp:rw,nosuid,size=16m",
		"--cap-drop", "ALL", "-e", "HOME=/tmp", "-e", "PYTHONDONTWRITEBYTECODE=1",
		"--security-opt", "no-new-privileges:true", "--user", "65532:65532",
		"--memory", memoryFlag(input.Spec.Resources.Memory), "--pids-limit", "64",
		"--network", "bridge", "-p", fmt.Sprintf("127.0.0.1::%d", input.Spec.Port), built.ImageID,
	}
	output, err := run(ctx, "", "docker", args...)
	if err != nil {
		return application.HealthResult{Reason: "container did not start: " + firstLine(output)}, nil
	}
	hostPort, ok := publishedHostPort(ctx, name, input.Spec.Port)
	if !ok {
		return application.HealthResult{Reason: "container has no loopback health port"}, nil
	}
	if reason := waitHealth(ctx, "127.0.0.1", hostPort); reason != "" {
		return application.HealthResult{Reason: reason}, nil
	}
	// Do not accept another process's response after the owned container
	// exited or when the daemon did not start the requested immutable image.
	observed, err := stdout(ctx, "docker", "inspect", "--format", "{{.Image}} {{.State.Running}}", name)
	if err != nil || strings.TrimSpace(string(observed)) != built.ImageID+" true" {
		return application.HealthResult{Reason: "healthy response did not match the running built image"}, nil
	}
	return application.HealthResult{Passed: true, Reason: "GET /health returned 200 for the running built image"}, nil
}

func publishedHostPort(ctx context.Context, name string, containerPort int) (int, bool) {
	mapped, err := stdout(ctx, "docker", "port", name, strconv.Itoa(containerPort)+"/tcp")
	if err == nil {
		if hostPort, ok := hostPortOf(string(mapped)); ok {
			return hostPort, true
		}
	}
	inspect, err := stdout(ctx, "docker", "inspect", "--format", "{{json .NetworkSettings.Ports}}", name)
	if err != nil {
		return 0, false
	}
	return hostPortFromPortsJSON(inspect, containerPort)
}

func hostPortFromPortsJSON(raw []byte, containerPort int) (int, bool) {
	var ports map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}
	if err := json.Unmarshal(raw, &ports); err != nil || len(ports) == 0 {
		return 0, false
	}
	key := strconv.Itoa(containerPort) + "/tcp"
	for _, binding := range ports[key] {
		if port, err := strconv.Atoi(strings.TrimSpace(binding.HostPort)); err == nil && port > 0 && port <= 65535 && binding.HostIP == "127.0.0.1" {
			return port, true
		}
	}
	return 0, false
}

func waitHealth(ctx context.Context, host string, port int) string {
	deadline := time.Now().Add(8 * time.Second)
	client := http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://%s/health", net.JoinHostPort(host, strconv.Itoa(port)))
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

func hostPortOf(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	line := strings.Split(value, "\n")[0]
	host, port, err := net.SplitHostPort(line)
	if err != nil {
		return 0, false
	}
	parsed, err := strconv.Atoi(port)
	if err != nil || host != "127.0.0.1" || parsed < 1 || parsed > 65535 {
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
