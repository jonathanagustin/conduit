package runfacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseReadsChartMemoryImageAndResourceExcerpt(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ab", 32)
	body := []byte("name: sample-app\nmemory-request: 256Mi\nmemory: 512Mi\nchart-values: memory: 512Mi\nresources: limits.memory=512Mi requests.memory=256Mi\nimage-ref: 127.0.0.1:5000/sample-app:0.2.0\nimage-digest: " + digest + "\n")
	got := Parse("sample-app-1", body)
	if got.ChartValues != "memory: 512Mi" || got.Resources != "limits.memory=512Mi requests.memory=256Mi" {
		t.Fatalf("chart = %#v", got)
	}
	if got.MemoryRequest != "256Mi" || got.MemoryLimit != "512Mi" || got.ImageRef != "127.0.0.1:5000/sample-app:0.2.0" || got.ImageDigest != digest {
		t.Fatalf("facts = %#v", got)
	}
}

func TestParseUsesMemoryAsTheResourceExcerpt(t *testing.T) {
	digest := "sha256:" + strings.Repeat("cd", 32)
	got := Parse("sample-app-1", []byte("name: sample-app\nmemory: 512Mi\nimage: 127.0.0.1:5000/sample-app:0.2.0@"+digest+"\n"))
	if got.ChartValues != "limits.memory=512Mi" || got.MemoryLimit != "512Mi" || got.ImageRef != "127.0.0.1:5000/sample-app:0.2.0" || got.ImageDigest != digest {
		t.Fatalf("excerpt = %#v", got)
	}
}

func TestLoadPrefersTheNamedSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source.txt"), []byte("memory: 8Gi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	named := "memory-request: 256Mi\nmemory: 512Mi\nchart-values: memory: 512Mi\nimage-ref: 127.0.0.1:5000/sample-app:0.2.0\nimage-digest: sha256:" + strings.Repeat("ef", 32) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sources", "sample-app-1.txt"), []byte(named), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Reader{Dir: dir}.Load(t.Context(), "sample-app-1")
	if err != nil || got.MemoryLimit != "512Mi" || got.ChartValues != "memory: 512Mi" {
		t.Fatalf("load = %#v %v", got, err)
	}
}
