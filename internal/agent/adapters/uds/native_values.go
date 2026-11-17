package uds

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"gopkg.in/yaml.v3"
)

// prepareZarfDeployment consumes the definition returned by the authenticated
// inspection of the same immutable source. Legacy packages retain variable
// flags; native packages receive the reviewed scalar JSON types in a private
// values file. CLI type inference must not turn "0012" or "false" into numbers
// or booleans. The caller keeps the file only for this Apply invocation.
func (e Executor) prepareZarfDeployment(step controlplane.PlanStep, source string, definition []byte) ([]string, func(), error) {
	noop := func() {}
	if len(definition) > 16<<20 {
		return nil, noop, refuse(step, "authenticated package definition exceeds the size limit")
	}
	var metadata struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Metadata   struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(definition))
	if err := decoder.Decode(&metadata); err != nil {
		return nil, noop, refuse(step, "authenticated package definition is invalid")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, noop, refuse(step, "authenticated package definition must contain one document")
	}
	if metadata.Kind != "ZarfPackageConfig" || metadata.Metadata.Name != step.Chart.PackageName {
		return nil, noop, refuse(step, "authenticated package identity does not match the reviewed package")
	}
	legacy := metadata.APIVersion == "zarf.dev/v1alpha1"
	if !legacy && metadata.APIVersion != "zarf.dev/v1beta1" {
		return nil, noop, refuse(step, "authenticated package API version is unsupported")
	}
	commands, err := e.deployCommands(step, source, legacy)
	if err != nil {
		return nil, noop, err
	}
	command := commands[len(commands)-1]
	if legacy {
		return command, noop, nil
	}
	// setFlags already admitted this one scalar object before authentication.
	// Compact preserves the JSON scalar types and bytes within string values.
	raw := bytes.TrimSpace(step.Values)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		raw = []byte("{}")
	}
	var values bytes.Buffer
	if err := json.Compact(&values, raw); err != nil {
		return nil, noop, refuse(step, "reviewed package values are invalid")
	}
	directory, err := os.MkdirTemp("", "conduit-native-values-")
	if err != nil {
		return nil, noop, refuse(step, "cannot prepare private package values")
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	path, err := filepath.Abs(filepath.Join(directory, "values.yaml"))
	if err != nil {
		cleanup()
		return nil, noop, refuse(step, "cannot resolve private package values")
	}
	if err := os.WriteFile(path, values.Bytes(), 0o600); err != nil {
		cleanup()
		return nil, noop, refuse(step, "cannot write private package values")
	}
	return append(command, "--values", path), cleanup, nil
}
