package domain

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/packagingtools"
)

func TestHelmTemplateWiresTheWorkloadAndPackage(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		if os.Getenv("PLATFORM_REQUIRE_HELM") == "1" {
			t.Fatal("PLATFORM_REQUIRE_HELM is set but helm is not on PATH")
		}
		t.Skip("helm is not installed")
	}
	spec := recordedSpec(t)
	rendered, err := Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("ab", 32)
	stamped, err := StampImage(rendered, "127.0.0.1:5000/hello-http", digest)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "chart")
	for _, file := range stamped.Chart {
		dest := filepath.Join(dir, file.Path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, file.Body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command(helm, "template", spec.Name, dir, "--namespace", spec.Name).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	report, err := packagingtools.New().ValidateManifests(context.Background(), string(out))
	if err != nil || !report.OK {
		t.Fatalf("workload gate: %v %+v", err, report.Findings)
	}
	docs := decodeDocs(t, out)
	deployment := docByKind(docs, "Deployment")
	service := docByKind(docs, "Service")
	if deployment == nil || service == nil {
		t.Fatalf("rendered kinds missing from %s", out)
	}
	if docByKind(docs, "ConfigMap") != nil {
		t.Fatal("rendered chart still contains a ConfigMap")
	}
	wantImage := "127.0.0.1:5000/hello-http@" + digest
	if containerImage(t, deployment) != wantImage {
		t.Fatalf("image = %s", containerImage(t, deployment))
	}
	if !nonRoot(deployment) {
		t.Fatal("deployment is not non-root")
	}
	if resource(t, deployment, "limits", "memory") != "64Mi" || resource(t, deployment, "limits", "cpu") != "50m" {
		t.Fatalf("limits memory=%s cpu=%s", resource(t, deployment, "limits", "memory"), resource(t, deployment, "limits", "cpu"))
	}
	if probePath(t, deployment, "readinessProbe") != "/health" || probePath(t, deployment, "livenessProbe") != "/health" {
		t.Fatal("probes do not call /health")
	}
	if metaName(t, service) != spec.Name || metaName(t, deployment) != spec.Name {
		t.Fatalf("names deploy=%s service=%s", metaName(t, deployment), metaName(t, service))
	}
	if mapText(labelMap(t, walk(t, service, "spec", "selector"))) != mapText(labelMap(t, walk(t, deployment, "spec", "selector", "matchLabels"))) {
		t.Fatal("service selector does not match the deployment")
	}
	if asInt(t, asMap(t, asSlice(t, walk(t, service, "spec", "ports"))[0])["port"]) != spec.Port {
		t.Fatal("service port does not match the spec")
	}
	expose := asMap(t, asSlice(t, walk(t, decodeOne(t, stamped.Package), "spec", "network", "expose"))[0])
	if text(expose["service"]) != metaName(t, service) {
		t.Fatalf("expose service = %v", expose["service"])
	}
	if asInt(t, expose["port"]) != spec.Port || asInt(t, expose["targetPort"]) != spec.Port {
		t.Fatalf("expose ports = %v %v", expose["port"], expose["targetPort"])
	}
	if mapText(labelMap(t, expose["selector"])) != mapText(labelMap(t, walk(t, service, "spec", "selector"))) {
		t.Fatal("package selector does not match the service")
	}
	dockerReport, err := packagingtools.New().ValidateDockerfile(context.Background(), string(stamped.Dockerfile), ".git\n")
	if err != nil || !dockerReport.OK {
		t.Fatalf("dockerfile gate: %v %+v", err, dockerReport.Findings)
	}
}

func TestZarfDevLintAcceptsTheStampedPackage(t *testing.T) {
	bin := strings.TrimSpace(os.Getenv("ZARF_BIN"))
	if bin == "" {
		path, err := exec.LookPath("zarf")
		if err != nil {
			t.Skip("zarf is not installed")
		}
		bin = path
	}
	spec := recordedSpec(t)
	rendered, err := Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("cd", 32)
	stamped, err := StampImage(rendered, "127.0.0.1:5000/hello-http", digest)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(rel string, body []byte) {
		t.Helper()
		dest := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Dockerfile", stamped.Dockerfile)
	write("main.go", []byte(spec.Files[0].Content))
	write("zarf.yaml", stamped.Zarf)
	write("uds-package.yaml", stamped.Package)
	write(".dockerignore", []byte(".git\n"))
	for _, file := range stamped.Chart {
		write(filepath.Join("chart", file.Path), file.Body)
	}
	cmd := exec.Command(bin, "dev", "lint", dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zarf dev lint: %v\n%s", err, out)
	}
}

func TestPythonAndHTMLDockerfilesPassTheGate(t *testing.T) {
	for _, files := range [][]SourceFile{
		{{Path: "main.py", Language: "python", Content: "print(1)\n"}},
		{{Path: "index.html", Language: "html", Content: "<p>Hello</p>\n"}},
	} {
		spec := recordedSpec(t)
		spec.Files = files
		rendered, err := Render(spec)
		if err != nil {
			t.Fatal(err)
		}
		report, err := packagingtools.New().ValidateDockerfile(context.Background(), string(rendered.Dockerfile), ".git\n")
		if err != nil || !report.OK {
			t.Fatalf("%s dockerfile gate: %v %+v\n%s", files[0].Path, err, report.Findings, rendered.Dockerfile)
		}
	}
}

func decodeDocs(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	var docs []map[string]any
	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(doc) > 0 {
			docs = append(docs, doc)
		}
	}
	return docs
}

func decodeOne(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func docByKind(docs []map[string]any, kind string) map[string]any {
	for _, doc := range docs {
		if text(doc["kind"]) == kind {
			return doc
		}
	}
	return nil
}

func containerImage(t *testing.T, deployment map[string]any) string {
	t.Helper()
	return text(container(t, deployment)["image"])
}

func nonRoot(deployment map[string]any) bool {
	pod, _ := walkMap(deployment, "spec", "template", "spec", "securityContext")
	containers, _ := walkAny(deployment, "spec", "template", "spec", "containers")
	list, _ := containers.([]any)
	if len(list) == 0 {
		return false
	}
	sc, _ := asMapOK(asMapOKMust(list[0])["securityContext"])
	return text(pod["runAsNonRoot"]) == "true" && text(sc["runAsNonRoot"]) == "true" && text(sc["runAsUser"]) == "65532"
}

func container(t *testing.T, deployment map[string]any) map[string]any {
	t.Helper()
	return asMap(t, asSlice(t, walk(t, deployment, "spec", "template", "spec", "containers"))[0])
}

func resource(t *testing.T, deployment map[string]any, section, key string) string {
	t.Helper()
	resources := asMap(t, container(t, deployment)["resources"])
	return text(asMap(t, resources[section])[key])
}

func probePath(t *testing.T, deployment map[string]any, name string) string {
	t.Helper()
	probe := asMap(t, container(t, deployment)[name])
	return text(asMap(t, probe["httpGet"])["path"])
}

func metaName(t *testing.T, doc map[string]any) string {
	t.Helper()
	return text(asMap(t, doc["metadata"])["name"])
}

func labelMap(t *testing.T, value any) map[string]string {
	t.Helper()
	raw := asMap(t, value)
	out := make(map[string]string, len(raw))
	for key, item := range raw {
		out[key] = text(item)
	}
	return out
}

func mapText(values map[string]string) string {
	parts := make([]string, 0, len(values))
	for key, value := range values {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ",")
}

func walk(t *testing.T, doc map[string]any, path ...string) any {
	t.Helper()
	var current any = doc
	for _, key := range path {
		current = asMap(t, current)[key]
	}
	return current
}

func walkMap(doc map[string]any, path ...string) (map[string]any, bool) {
	var current any = doc
	for _, key := range path {
		mapped, ok := asMapOK(current)
		if !ok {
			return nil, false
		}
		current = mapped[key]
	}
	return asMapOK(current)
}

func walkAny(doc map[string]any, path ...string) (any, bool) {
	var current any = doc
	for _, key := range path {
		mapped, ok := asMapOK(current)
		if !ok {
			return nil, false
		}
		current = mapped[key]
	}
	return current, true
}

func asMapOK(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}

func asMapOKMust(value any) map[string]any {
	mapped, _ := asMapOK(value)
	return mapped
}

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	mapped, ok := asMapOK(value)
	if !ok {
		t.Fatalf("not a map: %#v", value)
	}
	return mapped
}

func asSlice(t *testing.T, value any) []any {
	t.Helper()
	list, ok := value.([]any)
	if !ok {
		t.Fatalf("not a list: %#v", value)
	}
	return list
}

func asInt(t *testing.T, value any) int {
	t.Helper()
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case uint64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		t.Fatalf("not an int: %#v", value)
		return 0
	}
}

func text(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(value)
	}
}
