package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func recordedSpec(t *testing.T) Spec {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "adapters", "recorded", "testdata", "hello", "spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := ParseSpec(raw)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestRecordedSpecMatchesTheCheckedInSource(t *testing.T) {
	spec := recordedSpec(t)
	source, err := os.ReadFile(filepath.Join("..", "adapters", "recorded", "testdata", "hello", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Files) != 1 || spec.Files[0].Content != string(source) {
		t.Fatal("recorded spec does not carry the checked-in source")
	}
	if reason, ok := CheckAllowlist(spec); !ok {
		t.Fatal(reason)
	}
	if reason, ok := ScanSecretsAndSize(spec); !ok {
		t.Fatal(reason)
	}
	if reason, ok := AssessResources(spec); !ok {
		t.Fatal(reason)
	}
}

func TestParseSpecRejectsAnythingElse(t *testing.T) {
	spec := recordedSpec(t)
	raw := mustJSON(t, spec)
	if _, err := ParseSpec(append(raw, '\n', '{')); err == nil {
		t.Fatal("accepted extra data")
	}
	broken := strings.Replace(string(raw), `"port"`, `"portExtra"`, 1)
	if _, err := ParseSpec([]byte(broken)); err == nil {
		t.Fatal("accepted an unknown field")
	}
	spec.Routes = nil
	if _, err := ParseSpec(mustJSON(t, spec)); err == nil {
		t.Fatal("accepted a spec with no health route")
	}
}

func TestAllowlistRejectsUnknownPathsLanguagesAndRepeats(t *testing.T) {
	spec := recordedSpec(t)
	spec.Files[0].Path = "Dockerfile"
	if _, ok := CheckAllowlist(spec); ok {
		t.Fatal("accepted a packaging path")
	}
	spec = recordedSpec(t)
	spec.Files[0].Language = "python"
	if _, ok := CheckAllowlist(spec); ok {
		t.Fatal("accepted the wrong language")
	}
	spec = recordedSpec(t)
	spec.Files = append(spec.Files, spec.Files[0])
	if _, ok := CheckAllowlist(spec); ok {
		t.Fatal("accepted a repeated path")
	}
	spec = recordedSpec(t)
	spec.Files = append(spec.Files,
		SourceFile{Path: "main.py", Language: "python", Content: "print(1)\n"},
		SourceFile{Path: "index.html", Language: "html", Content: "<p>Hello</p>\n"},
		SourceFile{Path: "main.go", Language: "go", Content: spec.Files[0].Content},
	)
	if _, err := ParseSpec(mustJSON(t, spec)); err == nil {
		t.Fatal("accepted four files")
	}
}

func TestSecretScanAndSizeCeiling(t *testing.T) {
	spec := recordedSpec(t)
	spec.Files[0].Content += "\npassword=hunter2\n"
	if reason, ok := ScanSecretsAndSize(spec); ok || !strings.Contains(reason, "credential") {
		t.Fatalf("password = %q %v", reason, ok)
	}
	spec = recordedSpec(t)
	spec.Files[0].Content += "\n-----BEGIN PRIVATE KEY-----\n"
	if _, ok := ScanSecretsAndSize(spec); ok {
		t.Fatal("accepted a private key")
	}
	spec = recordedSpec(t)
	spec.Files[0].Content += strings.Repeat("a", MaxFileBytes)
	if reason, ok := ScanSecretsAndSize(spec); ok || !strings.Contains(reason, "bytes") {
		t.Fatalf("size = %q %v", reason, ok)
	}
	spec = recordedSpec(t)
	padding := MaxFileBytes - len(spec.Files[0].Content)
	if padding < 0 {
		t.Fatal("recorded source is already over the file ceiling")
	}
	spec.Files[0].Content += strings.Repeat("a", padding)
	if _, ok := ScanSecretsAndSize(spec); !ok {
		t.Fatal("rejected a file at exactly the size ceiling")
	}
}

func TestResourceCeilingBoundary(t *testing.T) {
	spec := recordedSpec(t)
	spec.Resources.Memory = "1024Mi"
	spec.Resources.CPU = "1000m"
	if _, ok := AssessResources(spec); !ok {
		t.Fatal("rejected the exact ceiling")
	}
	spec.Resources.Memory = "1Gi"
	if _, ok := AssessResources(spec); !ok {
		t.Fatal("rejected 1Gi")
	}
	spec.Resources.Memory = "1025Mi"
	if _, ok := AssessResources(spec); ok {
		t.Fatal("accepted memory over the ceiling")
	}
	spec.Resources.Memory = "2Gi"
	if reason, ok := AssessResources(spec); ok || !strings.Contains(reason, "1Gi") {
		t.Fatalf("2Gi = %q %v", reason, ok)
	}
	spec.Resources.Memory = "64Mi"
	spec.Resources.CPU = "1001m"
	if _, ok := AssessResources(spec); ok {
		t.Fatal("accepted cpu over the ceiling")
	}
	spec.Resources.CPU = "2"
	if _, ok := AssessResources(spec); ok {
		t.Fatal("accepted 2 cores")
	}
}

func TestRenderIsDeterministicAndOmitsSource(t *testing.T) {
	spec := recordedSpec(t)
	first, err := Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(spec)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Dockerfile) != string(second.Dockerfile) || string(first.ChartValues) != string(second.ChartValues) || string(first.Zarf) != string(second.Zarf) || string(first.Package) != string(second.Package) {
		t.Fatal("render is not deterministic")
	}
	for _, doc := range [][]byte{first.Dockerfile, first.ChartValues, first.Zarf, first.Package} {
		if strings.Contains(string(doc), "BEGIN PRIVATE KEY") || strings.Contains(string(doc), spec.Files[0].Content) {
			t.Fatal("rendered packaging contains source")
		}
	}
	if !strings.Contains(string(first.Dockerfile), "FROM scratch") || !strings.Contains(string(first.Dockerfile), "USER 65532:65532") {
		t.Fatalf("dockerfile = %s", first.Dockerfile)
	}
	if !strings.Contains(string(first.ChartValues), "memory: 64Mi") || !strings.Contains(string(first.Zarf), "CONDUIT_NATIVE") {
		t.Fatal("chart or package is missing the template fields")
	}
	source := PackagingSource(spec, "127.0.0.1:5000/hello-http:0.1.0", "sha256:"+strings.Repeat("ab", 32), "a small page")
	if !strings.Contains(string(source), "name: hello-http") || !strings.Contains(string(source), "memory: 64Mi") {
		t.Fatalf("source = %s", source)
	}
}

func TestDomainDoesNotImportProcesses(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{`"os/exec"`, `"os"`, `"net/http"`, `"/internal/`} {
			if strings.Contains(string(body), banned) {
				t.Fatalf("%s imports %s", name, banned)
			}
		}
	}
}

func mustJSON(t *testing.T, spec Spec) []byte {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
