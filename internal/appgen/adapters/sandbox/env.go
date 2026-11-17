package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/appgen/application"
)

const (
	// DefaultBuildTimeout bounds the compile and the image build together.
	DefaultBuildTimeout = 3 * time.Minute
	// MaxOutputBytes is how much of a child's output is kept.
	MaxOutputBytes = 64 << 10
	// feedbackBytes is how much failure output a build result carries.
	feedbackBytes = 4096
)

// buildAllowed is the set of parent variables the compiler and the container
// engine client may see. Names are compared in upper case. Everything else,
// including the Hub's DSNs, signing keys, OIDC secrets and model credentials,
// is dropped. This is the build's own list and is narrower than the model
// command's list in internal/localcmd: a build needs no provider credential.
var buildAllowed = map[string]bool{
	// Process basics. HOME is a path and lets the engine client find its
	// context. It is not a secret.
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true,
	"LANG": true, "LANGUAGE": true, "TZ": true, "TMPDIR": true, "TMP": true, "TEMP": true,
	"XDG_RUNTIME_DIR": true, "XDG_CACHE_HOME": true, "XDG_CONFIG_HOME": true,
	// Windows equivalents.
	"SYSTEMROOT": true, "COMSPEC": true, "PATHEXT": true, "USERPROFILE": true,
	"APPDATA": true, "LOCALAPPDATA": true,
	// Toolchain location and cache.
	"GOROOT": true, "GOCACHE": true,
	// Container engine endpoint selection.
	"DOCKER_HOST": true, "DOCKER_CONTEXT": true, "DOCKER_CONFIG": true,
	"DOCKER_TLS_VERIFY": true, "DOCKER_CERT_PATH": true, "DOCKER_BUILDKIT": true,
}

// buildFixed is set on every child after the filter, so the parent cannot
// change how the compiler resolves modules or toolchains.
var buildFixed = []string{
	"CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOPROXY=off", "GOTOOLCHAIN=local",
	"GOFLAGS=", "GOENV=off", "GOWORK=off", "GOSUMDB=off",
}

// BuildEnv is the explicit environment of a build child: the allowlisted
// parent variables followed by the fixed toolchain settings.
func BuildEnv(parent []string) []string {
	env := []string{}
	for _, item := range parent {
		key, _, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			continue
		}
		upper := strings.ToUpper(key)
		if buildAllowed[upper] || strings.HasPrefix(upper, "LC_") {
			env = append(env, item)
		}
	}
	return append(env, buildFixed...)
}

// Available reports whether this host can build any shape at all. Every
// shape needs the container engine client. The Hub's shipped image is FROM
// scratch and does not hold it, so there this returns an error that wraps
// application.ErrBuildExecutorUnavailable.
//
// The compiler is needed only for main.go and index.html, and the shape is
// not known until the model replies. A host with the engine and no compiler
// is therefore available here, can build main.py, and reports the missing
// compiler from Build when a reply needs it.
func (Docker) Available() error {
	if _, err := exec.LookPath("docker"); err == nil {
		return nil
	}
	detail := "app generation needs the docker binary on the Hub host, and it was not found"
	if _, err := exec.LookPath("go"); err != nil {
		detail = "app generation needs the docker binary on the Hub host, and go for main.go and index.html; neither was found"
	}
	return fmt.Errorf("%w: %s", application.ErrBuildExecutorUnavailable, detail)
}

// capped keeps the first limit bytes written to it and counts the rest.
type capped struct {
	buffer  bytes.Buffer
	limit   int
	dropped int
}

func (writer *capped) Write(raw []byte) (int, error) {
	room := writer.limit - writer.buffer.Len()
	if room >= len(raw) {
		writer.buffer.Write(raw)
		return len(raw), nil
	}
	if room > 0 {
		writer.buffer.Write(raw[:room])
	}
	writer.dropped += len(raw) - max(room, 0)
	return len(raw), nil
}

func (writer *capped) Bytes() []byte {
	if writer.dropped == 0 {
		return writer.buffer.Bytes()
	}
	return append(writer.buffer.Bytes(), []byte("\n[output truncated]")...)
}

// run starts one child in dir with the build environment and returns its
// capped combined output. dir may be empty for a command that needs none.
func run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = BuildEnv(os.Environ())
	cmd.WaitDelay = 2 * time.Second
	out := &capped{limit: MaxOutputBytes}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	return out.Bytes(), err
}

// stdout is run for a command whose standard output is the answer.
func stdout(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = BuildEnv(os.Environ())
	cmd.WaitDelay = 2 * time.Second
	out := &capped{limit: MaxOutputBytes}
	cmd.Stdout = out
	err := cmd.Run()
	return out.Bytes(), err
}

// unavailable turns a missing binary into the deployment error. Any other
// start failure is returned unchanged.
func unavailable(name string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: the %s binary was not found on the Hub host", application.ErrBuildExecutorUnavailable, name)
	}
	return nil
}

func feedback(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > feedbackBytes {
		text = text[:feedbackBytes] + "\n[output truncated]"
	}
	return strings.ToValidUTF8(text, "?")
}
