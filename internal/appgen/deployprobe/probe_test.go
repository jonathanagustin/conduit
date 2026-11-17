package deployprobe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/registry"
	"github.com/jonathanagustin/conduit/internal/appgen/adapters/sandbox"
	"github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/appgen/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
)

func TestDeployGeneratedPackage(t *testing.T) {
	if os.Getenv("CONDUIT_APPGEN_DEPLOY") != "1" {
		t.Skip("set CONDUIT_APPGEN_DEPLOY=1 to deploy the generated package on k3d or kind")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	for _, bin := range []string{"docker", "zarf", "kubectl"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("%s is not on PATH", bin)
		}
	}
	cluster := clusterTool()
	if cluster == "" {
		t.Fatal("k3d or kind is not on PATH")
	}
	spec := probeSpec(t)
	rendered, err := domain.Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]byte{}
	for _, file := range spec.Files {
		sources[file.Path] = []byte(file.Content)
	}
	built, err := (sandbox.Docker{}).Build(ctx, application.BuildRequest{Spec: spec, Dockerfile: rendered.Dockerfile, Sources: sources})
	if err != nil || !built.Passed {
		t.Fatalf("build = %#v %v", built, err)
	}
	health, err := (sandbox.Docker{}).Check(ctx, built, application.BuildRequest{Spec: spec, Dockerfile: rendered.Dockerfile, Sources: sources})
	if err != nil || !health.Passed {
		t.Fatalf("health = %#v %v", health, err)
	}
	fmt.Println("appgen deploy probe: image built")
	registryName := "conduit-appgen-registry"
	run(t, ctx, "docker", "rm", "-f", registryName)
	if out, err := command(ctx, "docker", "run", "-d", "--name", registryName, "-p", "127.0.0.1:5001:5000", "registry:2"); err != nil {
		t.Fatalf("registry: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = command(context.Background(), "docker", "rm", "-f", registryName) })
	waitRegistry(t, ctx)
	ref, digest, _, err := (registry.DockerPublisher{Registry: "127.0.0.1:5001"}).Push(ctx, built.ImageID, spec.Name, rendered.ChartValues)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	stamped, err := domain.StampImage(rendered, domain.ImageRepository(ref), digest)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFile(t, dir, "Dockerfile", stamped.Dockerfile)
	writeFile(t, dir, "zarf.yaml", stamped.Zarf)
	writeFile(t, dir, "uds-package.yaml", stamped.Package)
	writeFile(t, dir, ".dockerignore", []byte(".git\n"))
	for _, file := range spec.Files {
		writeFile(t, dir, file.Path, []byte(file.Content))
	}
	for _, file := range stamped.Chart {
		writeFile(t, dir, filepath.Join("chart", file.Path), file.Body)
	}
	if out, err := command(ctx, "zarf", "dev", "lint", dir); err != nil {
		t.Fatalf("zarf dev lint: %v\n%s", err, out)
	}
	outDir := filepath.Join(dir, "out")
	archive, err := (packagingtools.ZarfRunner{}).Create(ctx, dir, outDir)
	if err != nil || strings.TrimSpace(archive) == "" {
		t.Fatalf("zarf package create: %v", err)
	}
	fmt.Println("appgen deploy probe: package created")
	kubeconfig := createCluster(t, ctx, cluster)
	t.Cleanup(func() { deleteCluster(cluster) })
	apply := exec.CommandContext(ctx, "kubectl", "apply", "--validate=false", "-f", "-")
	apply.Env = kubeEnv(kubeconfig)
	apply.Stdin = strings.NewReader(packageCRD)
	if out, err := apply.CombinedOutput(); err != nil {
		t.Fatalf("package crd: %v\n%s", err, out)
	}
	initDir := t.TempDir()
	if out, err := command(ctx, "zarf", "package", "pull", "oci://ghcr.io/zarf-dev/packages/init:v1.0.0", "-o", initDir); err != nil {
		t.Fatalf("zarf init package: %v\n%s", err, out)
	}
	initPkg := ""
	entries, err := os.ReadDir(initDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar.zst") {
			initPkg = filepath.Join(initDir, entry.Name())
		}
	}
	if initPkg == "" {
		t.Fatal("zarf init package was not downloaded")
	}
	fmt.Println("appgen deploy probe: cluster ready")
	if out, err := commandEnv(ctx, kubeEnv(kubeconfig), "zarf", "init", "--confirm", "--agent-mutation-policy=all", initPkg); err != nil {
		t.Fatalf("zarf init: %v\n%s", err, out)
	}
	fmt.Println("appgen deploy probe: zarf initialized")
	if out, err := commandEnv(ctx, kubeEnv(kubeconfig), "zarf", "package", "deploy", archive, "--confirm"); err != nil {
		t.Fatalf("zarf package deploy: %v\n%s", err, out)
	}
	if out, err := kubectl(ctx, kubeconfig, "-n", spec.Name, "rollout", "status", "deploy/"+spec.Name, "--timeout=180s"); err != nil {
		listed, _ := kubectl(ctx, kubeconfig, "get", "deploy,pods,svc", "-A")
		t.Fatalf("rollout: %v\n%s\n%s", err, out, listed)
	}
	if out, err := kubectl(ctx, kubeconfig, "-n", spec.Name, "get", "packages.uds.dev", spec.Name); err != nil {
		t.Fatalf("package resource: %v\n%s", err, out)
	}
	status := requestApp(t, ctx, kubeconfig, spec.Name, spec.Port)
	fmt.Printf("appgen deploy probe: GET / HTTP %d\n", status)
	if status != http.StatusOK {
		t.Fatalf("GET / returned %d", status)
	}
}

func probeSpec(t *testing.T) domain.Spec {
	t.Helper()
	spec, _, err := (recorded.Adapter{}).Generate(context.Background(), domain.Request{Name: "hello-http", Description: "a small page"})
	if err != nil {
		t.Fatal(err)
	}
	switch strings.TrimSpace(os.Getenv("APPGEN_SHAPE")) {
	case "", "go":
		return spec
	case "html":
		spec.Files = []domain.SourceFile{{Path: "index.html", Language: "html", Content: "<!doctype html><title>Hello</title><p>Hello</p>\n"}}
		return spec
	case "python":
		spec.Files = []domain.SourceFile{{Path: "main.py", Language: "python", Content: pythonApp}}
		return spec
	default:
		t.Fatalf("APPGEN_SHAPE must be go, python, or html")
		return domain.Spec{}
	}
}

func clusterTool() string {
	if _, err := exec.LookPath("k3d"); err == nil {
		return "k3d"
	}
	if _, err := exec.LookPath("kind"); err == nil {
		return "kind"
	}
	return ""
}

func createCluster(t *testing.T, ctx context.Context, tool string) string {
	t.Helper()
	deleteCluster(tool)
	switch tool {
	case "k3d":
		confDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(confDir, "00-cgroupv1.conf"), []byte(kubeletCgroupV1), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := command(ctx, "k3d", "cluster", "create", "conduit-appgen", "--wait", "--agents", "0",
			"--k3s-arg", "--flannel-backend=host-gw@server:0",
			"--k3s-arg", "--disable=traefik@server:0",
			"--k3s-arg", "--disable=metrics-server@server:0",
			"--k3s-arg", "--disable=coredns@server:0",
			"--k3s-arg", "--disable=local-storage@server:0",
			"--volume", confDir+":/etc/rancher/k3s/kubelet.conf.d@server:0",
			"--k3s-arg", "--kubelet-arg=config-dir=/etc/rancher/k3s/kubelet.conf.d@server:0",
		); err != nil {
			t.Fatalf("k3d create: %v\n%s", err, out)
		}
		installRuncScoreFix(t, ctx)
		importImage(t, ctx, "k3d-conduit-appgen-server-0", "rancher/mirrored-pause:3.6")
		return k3dKubeconfig(t, ctx)
	case "kind":
		if out, err := command(ctx, "kind", "create", "cluster", "--name", "conduit-appgen"); err != nil {
			t.Fatalf("kind create: %v\n%s", err, out)
		}
		out, err := command(ctx, "kind", "get", "kubeconfig", "--name", "conduit-appgen")
		if err != nil {
			t.Fatalf("kind kubeconfig: %v\n%s", err, out)
		}
		path := filepath.Join(t.TempDir(), "kubeconfig")
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	default:
		t.Fatalf("unknown cluster tool %s", tool)
		return ""
	}
}

func k3dKubeconfig(t *testing.T, ctx context.Context) string {
	t.Helper()
	out, err := command(ctx, "k3d", "kubeconfig", "get", "conduit-appgen")
	if err != nil {
		t.Fatalf("k3d kubeconfig: %v\n%s", err, out)
	}
	published := strings.ReplaceAll(out, "https://0.0.0.0:", "https://127.0.0.1:")
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(published), 0o600); err != nil {
		t.Fatal(err)
	}
	if kubeconfigWorks(ctx, path) {
		return path
	}
	ipOut, err := command(ctx, "docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", "k3d-conduit-appgen-server-0")
	if err != nil {
		t.Fatalf("node address: %v\n%s", err, ipOut)
	}
	ip := strings.TrimSpace(ipOut)
	rewritten := rewriteServer(published, "https://"+ip+":6443")
	if err := os.WriteFile(path, []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForNode(t, ctx, path)
	return path
}

func rewriteServer(kubeconfig, server string) string {
	lines := strings.Split(kubeconfig, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" || !strings.Contains(line, "server:") {
			continue
		}
		prefix := line[:strings.Index(line, "server:")]
		lines[i] = prefix + "server: " + server
	}
	return strings.Join(lines, "\n")
}

func kubeconfigWorks(ctx context.Context, kubeconfig string) bool {
	check, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	_, err := kubectl(check, kubeconfig, "--request-timeout=8s", "get", "nodes")
	return err == nil
}

func importImage(t *testing.T, ctx context.Context, node, ref string) {
	t.Helper()
	if out, err := command(ctx, "docker", "pull", ref); err != nil {
		t.Fatalf("pull %s: %v\n%s", ref, err, out)
	}
	save := exec.CommandContext(ctx, "docker", "save", ref)
	load := exec.CommandContext(ctx, "docker", "exec", "-i", node, "ctr", "-n", "k8s.io", "images", "import", "-")
	pipe, err := save.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	load.Stdin = pipe
	var loaded strings.Builder
	load.Stdout = &loaded
	load.Stderr = &loaded
	if err := load.Start(); err != nil {
		t.Fatal(err)
	}
	if err := save.Run(); err != nil {
		_ = load.Wait()
		t.Fatalf("save %s: %v", ref, err)
	}
	if err := load.Wait(); err != nil {
		t.Fatalf("import %s: %v\n%s", ref, err, loaded.String())
	}
}

func waitForNode(t *testing.T, ctx context.Context, kubeconfig string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		out, err := kubectl(ctx, kubeconfig, "get", "nodes")
		last = out
		if err == nil && strings.Contains(out, " Ready") {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("node did not become ready: %s", last)
}

func installRuncScoreFix(t *testing.T, ctx context.Context) {
	t.Helper()
	node := "k3d-conduit-appgen-server-0"
	wrapper := filepath.Join(t.TempDir(), "runc")
	if err := os.WriteFile(wrapper, []byte(runcScoreFix), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := command(ctx, "docker", "exec", node, "sh", "-c", "if [ ! -e /bin/runc.real ]; then cp /bin/runc /bin/runc.real; fi"); err != nil {
		t.Fatalf("save node runc: %v\n%s", err, out)
	}
	if out, err := command(ctx, "docker", "cp", wrapper, node+":/bin/runc"); err != nil {
		t.Fatalf("install runc wrapper: %v\n%s", err, out)
	}
	if out, err := command(ctx, "docker", "exec", node, "chmod", "755", "/bin/runc"); err != nil {
		t.Fatalf("chmod runc wrapper: %v\n%s", err, out)
	}
}

func deleteCluster(tool string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch tool {
	case "k3d":
		_, _ = command(ctx, "k3d", "cluster", "delete", "conduit-appgen")
	case "kind":
		_, _ = command(ctx, "kind", "delete", "cluster", "--name", "conduit-appgen")
	}
}

func requestApp(t *testing.T, ctx context.Context, kubeconfig, name string, port int) int {
	t.Helper()
	forward := exec.CommandContext(ctx, "kubectl", "-n", name, "port-forward", "svc/"+name, fmt.Sprintf("18080:%d", port))
	forward.Env = kubeEnv(kubeconfig)
	stdout, err := forward.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	forward.Stderr = forward.Stdout
	if err := forward.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = forward.Process.Kill()
		_, _ = forward.Process.Wait()
	}()
	ready := make(chan struct{})
	go func() {
		defer close(ready)
		buf := make([]byte, 256)
		var got []byte
		for {
			n, readErr := stdout.Read(buf)
			got = append(got, buf[:n]...)
			if strings.Contains(string(got), "Forwarding from") || readErr != nil {
				return
			}
		}
	}()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("port-forward did not become ready")
	}
	client := http.Client{Timeout: 5 * time.Second}
	health, err := client.Get("http://127.0.0.1:18080/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(health.Body)
	health.Body.Close()
	if health.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		t.Fatalf("GET /health = %d %q", health.StatusCode, body)
	}
	page, err := client.Get("http://127.0.0.1:18080/")
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	return page.StatusCode
}

func waitRegistry(t *testing.T, ctx context.Context) {
	t.Helper()
	client := http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		response, err := client.Get("http://127.0.0.1:5001/v2/")
		if err == nil {
			response.Body.Close()
			if response.StatusCode < 300 {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("registry did not accept connections")
}

func writeFile(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	dest := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func kubectl(ctx context.Context, kubeconfig string, args ...string) (string, error) {
	return commandEnv(ctx, kubeEnv(kubeconfig), append([]string{"kubectl"}, args...)...)
}

func kubeEnv(kubeconfig string) []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+1)
	for _, entry := range base {
		if strings.HasPrefix(entry, "KUBECONFIG=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "KUBECONFIG="+kubeconfig)
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	return commandEnv(ctx, os.Environ(), append([]string{name}, args...)...)
}

func commandEnv(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func run(t *testing.T, ctx context.Context, name string, args ...string) {
	t.Helper()
	_, _ = command(ctx, name, args...)
}

const kubeletCgroupV1 = `apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
failCgroupV1: false
`

const runcScoreFix = `#!/bin/sh
REAL=/bin/runc.real
bundle=""
prev=""
for a in "$@"; do
  [ "$prev" = "--bundle" ] || [ "$prev" = "-b" ] && bundle="$a"
  prev="$a"
done
if [ -n "$bundle" ] && [ -f "$bundle/config.json" ]; then
  sed -i 's/"oomScoreAdj":-[0-9]*/"oomScoreAdj":0/g' "$bundle/config.json"
fi
prev=""
for a in "$@"; do
  [ "$prev" = "--process" ] || [ "$prev" = "-p" ] && [ -f "$a" ] && sed -i 's/"oomScoreAdj":-[0-9]*/"oomScoreAdj":0/g' "$a"
  prev="$a"
done
exec "$REAL" "$@"
`

const packageCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: packages.uds.dev
spec:
  group: uds.dev
  scope: Namespaced
  names:
    kind: Package
    listKind: PackageList
    plural: packages
    singular: package
  versions:
    - name: v1alpha1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          x-kubernetes-preserve-unknown-fields: true
`

const pythonApp = `import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            body = b"ok"
        elif self.path == "/":
            body = b"<!doctype html><title>Hello</title><p>Hello</p>"
        else:
            self.send_response(404)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        return

port = int(os.environ.get("PORT", "8080"))
ThreadingHTTPServer(("0.0.0.0", port), Handler).serve_forever()
`
