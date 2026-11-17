package uds

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func nativeBundleConfiguration(step controlplane.PlanStep) ([]byte, error) {
	if _, err := setFlags(step, "--set", zarfVariableName); err != nil {
		return nil, err
	}
	raw := bytes.TrimSpace(step.Values)
	values := map[string]json.RawMessage{}
	if len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, refuse(step, "invalid native bundle variables")
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var text strings.Builder
	text.WriteString("variables = {\n")
	for _, key := range keys {
		encodedKey, _ := json.Marshal(key)
		value := bytes.TrimSpace(values[key])
		if len(value) > 0 && value[0] == '"' {
			var scalar string
			if err := json.Unmarshal(value, &scalar); err != nil {
				return nil, refuse(step, "invalid native bundle string")
			}
			encoded, _ := json.Marshal(scalar)
			// HCL quoted strings interpolate these sequences even when the
			// surrounding string is valid JSON. Preserve them as literal data.
			value = []byte(strings.ReplaceAll(strings.ReplaceAll(string(encoded), "${", "$${"), "%{", "%%{"))
		}
		text.Write(encodedKey)
		text.WriteString(" = ")
		text.Write(value)
		text.WriteByte('\n')
	}
	text.WriteString("}\n")
	return []byte(text.String()), nil
}

func prepareNativeBundleDeployment(step controlplane.PlanStep, command []string) ([]string, func(), error) {
	noop := func() {}
	contents, err := nativeBundleConfiguration(step)
	if err != nil {
		return nil, noop, err
	}
	directory, err := os.MkdirTemp("", "conduit-native-bundle-")
	if err != nil {
		return nil, noop, refuse(step, "cannot prepare private bundle configuration")
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	path, err := filepath.Abs(filepath.Join(directory, "config.uds.hcl"))
	if err != nil {
		cleanup()
		return nil, noop, refuse(step, "cannot resolve private bundle configuration")
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		cleanup()
		return nil, noop, refuse(step, "cannot write private bundle configuration")
	}
	return append(command, "--config", path), cleanup, nil
}
