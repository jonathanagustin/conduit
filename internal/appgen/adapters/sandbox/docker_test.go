package sandbox

import (
	"context"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/appgen/application"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryFlagUsesMebibytes(t *testing.T) {
	if memoryFlag("64Mi") != "64m" || memoryFlag("1Gi") != "1024m" {
		t.Fatalf("64Mi=%s 1Gi=%s", memoryFlag("64Mi"), memoryFlag("1Gi"))
	}
}

func TestHostPortParser(t *testing.T) {
	port, ok := hostPortOf("127.0.0.1:34567\n")
	if !ok || port != 34567 {
		t.Fatalf("port = %d %v", port, ok)
	}
	if _, ok := hostPortOf("not a port"); ok {
		t.Fatal("accepted a bad port line")
	}
}

func TestHostPortFromPortsJSON(t *testing.T) {
	port, ok := hostPortFromPortsJSON([]byte(`{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"34567"}]}`), 8080)
	if !ok || port != 34567 {
		t.Fatalf("port = %d %v", port, ok)
	}
	if _, ok := hostPortFromPortsJSON([]byte(`{}`), 8080); ok {
		t.Fatal("empty Ports looked published")
	}
	if _, ok := hostPortFromPortsJSON([]byte("null"), 8080); ok {
		t.Fatal("null Ports looked published")
	}
}

func TestHealthIsBoundToOwnedImmutableImage(t *testing.T) {
	image := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer server.Close()
	port := strings.TrimPrefix(server.URL, "http://127.0.0.1:")
	for _, tc := range []struct {
		name, observed, runFail, ports string
		passed                         bool
	}{
		{"running exact image", image + " true", "", "127.0.0.1:" + port, true},
		{"different image", "sha256:" + strings.Repeat("b", 64) + " true", "", "127.0.0.1:" + port, false},
		{"stopped image", image + " false", "", "127.0.0.1:" + port, false},
		{"bridge unavailable", image + " true", "1", "", false},
		{"no bridge endpoint", image + " true", "", "", false},
		{"nonloopback endpoint", image + " true", "", "0.0.0.0:" + port, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			log := filepath.Join(dir, "commands")
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$COMMAND_LOG"
case "$1" in
run) if [ "$FAIL_RUN" = 1 ]; then echo 'bridge unavailable'; exit 1; fi; echo owned-container ;;
port) printf '%s\n' "$PUBLISHED_PORT" ;;
inspect) case "$*" in *NetworkSettings*) echo '{}' ;; *) printf '%s\n' "$OBSERVED_IMAGE" ;; esac ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
			t.Setenv("COMMAND_LOG", log)
			t.Setenv("OBSERVED_IMAGE", tc.observed)
			t.Setenv("FAIL_RUN", tc.runFail)
			t.Setenv("PUBLISHED_PORT", tc.ports)
			var names []string
			for range 2 {
				result, err := (Docker{}).Check(context.Background(), application.BuildResult{Passed: true, ImageID: image}, application.BuildRequest{})
				if err != nil || result.Passed != tc.passed {
					t.Fatalf("health=%+v err=%v", result, err)
				}
			}
			raw, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			for i, line := range lines {
				if strings.Contains(line, "--network host") || strings.Contains(line, "appgen-check-hello") {
					t.Fatalf("unsafe command: %s", line)
				}
				if strings.HasPrefix(line, "run ") {
					if !strings.HasSuffix(line, " "+image) {
						t.Fatalf("mutable image: %s", line)
					}
					fields := strings.Fields(line)
					for j, f := range fields {
						if f == "--name" {
							names = append(names, fields[j+1])
						}
					}
					if i != 0 && !strings.HasPrefix(lines[i-1], "rm -f ") {
						t.Fatalf("unexpected pre-start cleanup: %v", lines)
					}
				}
			}
			if len(names) != 2 || names[0] == names[1] {
				t.Fatalf("container identities reused: %v", names)
			}
			for _, name := range names {
				if !strings.Contains(string(raw), "rm -f "+name+"\n") {
					t.Fatalf("owned container not cleaned: %s", name)
				}
			}
		})
	}
}

func TestUnverifiedImageDoesNotInvokeDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, built := range []application.BuildResult{{Passed: false, ImageID: "sha256:" + strings.Repeat("a", 64)}, {Passed: true, ImageID: "appgen-hello:local"}, {Passed: true, ImageID: ""}} {
		result, err := (Docker{}).Check(context.Background(), built, application.BuildRequest{})
		if err != nil || result.Passed || result.Reason != "no verified image to start" {
			t.Fatalf("health=%+v err=%v", result, err)
		}
	}
}

func TestPortsRejectUnconfinedAndInvalidBindings(t *testing.T) {
	for _, host := range []string{"0.0.0.0", "::", "192.0.2.1", ""} {
		if _, ok := hostPortOf(host + ":34567"); ok {
			t.Fatalf("accepted host %q", host)
		}
		raw := []byte(fmt.Sprintf(`{"8080/tcp":[{"HostIp":%q,"HostPort":"34567"}]}`, host))
		if _, ok := hostPortFromPortsJSON(raw, 8080); ok {
			t.Fatalf("accepted binding %s", raw)
		}
	}
	for _, port := range []string{"0", "-1", "65536"} {
		if _, ok := hostPortOf("127.0.0.1:" + port); ok {
			t.Fatalf("accepted port %s", port)
		}
	}
}

func TestBuildUsesEngineRecordedImageID(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "commands")
	image := "sha256:" + strings.Repeat("c", 64)
	for name, script := range map[string]string{
		"go": "#!/bin/sh\nprintf compiled > app\n",
		"docker": `#!/bin/sh
printf '%s\n' "$*" >> "$COMMAND_LOG"
while [ "$#" -gt 0 ]; do
 if [ "$1" = --iidfile ]; then shift; printf '%s\n' "$BUILT_IMAGE" > "$1"; exit 0; fi
 shift
done
exit 1
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("COMMAND_LOG", log)
	input := application.BuildRequest{Sources: map[string][]byte{"main.go": []byte("package main")}}
	for _, id := range []string{image, "mutable:tag", "sha256:invalid"} {
		t.Setenv("BUILT_IMAGE", id)
		result, err := (Docker{}).Build(context.Background(), input)
		if err != nil || result.Passed != (id == image) {
			t.Fatalf("build=%+v err=%v", result, err)
		}
		if result.Passed && result.ImageID != image {
			t.Fatalf("wrong image: %+v", result)
		}
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	tags := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !strings.Contains(line, "--network none") || !strings.Contains(line, "--iidfile ") {
			t.Fatalf("build unbound: %s", line)
		}
		fields := strings.Fields(line)
		for j, f := range fields {
			if f == "--tag" {
				if tags[fields[j+1]] {
					t.Fatalf("shared build tag: %s", line)
				}
				tags[fields[j+1]] = true
			}
		}
	}
	if len(tags) != 3 {
		t.Fatalf("unexpected build invocations: %s", raw)
	}
}
