package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestProfileSchemaIsVersionedAndExposedByCLI(t *testing.T) {
	t.Parallel()
	var schema map[string]any
	if err := json.Unmarshal(conduitProfileSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("schema dialect=%v", schema["$schema"])
	}
	properties := schema["properties"].(map[string]any)
	version := properties["version"].(map[string]any)
	if version["const"] != float64(1) {
		t.Fatalf("schema version=%v", version["const"])
	}
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"profile", "schema"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != strings.TrimSpace(string(conduitProfileSchema)) {
		t.Fatal("profile schema output differs from the embedded contract")
	}
}

// A profile's output field names the formats the CLI renders, and the schema is
// the durable copy of that list -- a profile outlives the session that wrote
// it, so a value accepted here is one the CLI has to keep honouring. `pretty`
// was in both lists and rendered YAML, so it left both together.
func TestProfileOutputAcceptsOnlyTheFormatsConduitRenders(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		output    string
		wantError bool
	}{
		{output: ""},
		{output: "json"},
		{output: "yaml"},
		{output: "YAML"},
		{output: "table"},
		{output: "TABLE"},
		{output: "pretty", wantError: true},
	} {
		profile := connectionProfile{
			Name: "hub", Server: "https://hub.example.test", Output: test.output,
			SPIFFEEndpoint: "unix:///run/spire/agent.sock", HubSPIFFEID: "spiffe://platform.delivery/hub",
		}
		err := profile.validate("hub")
		if (err != nil) != test.wantError {
			t.Errorf("output %q: err = %v, want error = %v", test.output, err, test.wantError)
		}
		if test.wantError && err != nil && !strings.Contains(err.Error(), "json, yaml, or table") {
			t.Errorf("output %q: err = %v, want the formats that do exist", test.output, err)
		}
	}

	var schema struct {
		Defs struct {
			Profile struct {
				Properties struct {
					Output struct {
						Enum []string `json:"enum"`
					} `json:"output"`
				} `json:"properties"`
			} `json:"profile"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(conduitProfileSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if enum := schema.Defs.Profile.Properties.Output.Enum; strings.Join(enum, ",") != "json,yaml,table" {
		t.Errorf("schema output enum = %v, want the profile formats writeOutput renders", enum)
	}
}

func TestCompleteProfileDocumentLifecycle(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config", "conduit.yaml")
	run := func(arguments ...string) (string, error) {
		var output bytes.Buffer
		runtime := Runtime{
			Stdout: &output,
			Stderr: &output,
			Getenv: func(name string) string {
				if name == "CONDUIT_CONFIG_FILE" {
					return configPath
				}
				return ""
			},
		}
		root := NewRoot(runtime)
		root.SetArgs(arguments)
		err := root.Execute()
		return output.String(), err
	}

	_, err := run(
		"--profile", "production",
		"--server", "https://hub.example.test",
		"--timeout", "45s",
		"--output", "yaml",
		"--ca-file", "/run/conduit/ca.pem",
		"--client-cert-file", "/run/conduit/client.crt",
		"--client-key-file", "/run/conduit/client.key",
		"configure",
	)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode=%#o", info.Mode().Perm())
	}
	parent, err := os.Stat(filepath.Dir(configPath))
	if err != nil {
		t.Fatal(err)
	}
	if parent.Mode().Perm() != 0o700 {
		t.Fatalf("config directory mode=%#o", parent.Mode().Perm())
	}

	_, err = run(
		"--server", "https://hub.example.test",
		"--timeout", "1m",
		"--output", "json",
		"--spiffe-endpoint", "unix:///run/spire/agent.sock",
		"--hub-spiffe-id", "spiffe://platform.delivery/hub",
		"profile", "create", "spoke",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run("profile", "use", "spoke"); err != nil {
		t.Fatal(err)
	}
	if _, err := run("profile", "rename", "spoke", "aws"); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var document profileDocument
	if err := yaml.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	want := profileDocument{
		Version: 1,
		Current: "aws",
		Profiles: map[string]connectionProfile{
			"production": {
				Name: "production", Server: "https://hub.example.test", Timeout: "45s", Output: "yaml",
				CAFile: "/run/conduit/ca.pem", ClientCertFile: "/run/conduit/client.crt", ClientKeyFile: "/run/conduit/client.key",
			},
			"aws": {
				Name: "aws", Server: "https://hub.example.test", Timeout: "1m0s", Output: "json",
				SPIFFEEndpoint: "unix:///run/spire/agent.sock", HubSPIFFEID: "spiffe://platform.delivery/hub",
			},
		},
	}
	if !reflect.DeepEqual(document, want) {
		t.Fatalf("document=%#v, want %#v", document, want)
	}
	output, err := run("profile", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, `"current": "aws"`) || !strings.Contains(output, `"aws"`) || !strings.Contains(output, `"production"`) {
		t.Fatalf("profile list output=%s", output)
	}
}

func TestProfileDocumentRejectsUnsafeOrAmbiguousConfiguration(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"unknown field": `version: 1
profiles: {}
unexpected: true
`,
		"multiple documents": `version: 1
profiles: {}
---
version: 1
profiles: {}
`,
		"missing current": `version: 1
current: absent
profiles: {}
`,
		"map key mismatch": `version: 1
profiles:
  expected:
    name: different
    server: https://hub.example.test
    caFile: /ca.pem
    clientCertFile: /client.crt
    clientKeyFile: /client.key
`,
		"partial identity": `version: 1
profiles:
  broken:
    name: broken
    server: https://hub.example.test
    caFile: /ca.pem
`,
		// A profile's timeout is a duration string on disk. Skipping the parse
		// leaves the profile usable on the default timeout, so a typo here
		// costs nothing at write time and shows up much later as commands that
		// give up at a moment nobody configured.
		"unparseable timeout": `version: 1
profiles:
  broken:
    name: broken
    server: https://hub.example.test
    timeout: soon
    caFile: /ca.pem
    clientCertFile: /client.crt
    clientKeyFile: /client.key
`,
		"invalid output": `version: 1
profiles:
  broken:
    name: broken
    server: https://hub.example.test
    output: xml
    caFile: /ca.pem
    clientCertFile: /client.crt
    clientKeyFile: /client.key
`,
	}
	for name, contents := range tests {
		name, contents := name, contents
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			root := NewRoot(Runtime{
				Stdout: &output,
				Stderr: &output,
				Getenv: func(key string) string {
					if key == "CONDUIT_CONFIG_FILE" {
						return configPath
					}
					return ""
				},
			})
			root.SetArgs([]string{"profile", "list"})
			if err := root.Execute(); err == nil {
				t.Fatal("invalid complete profile document was accepted")
			}
		})
	}
}

// The first profile added becomes the current one; later ones do not displace
// it. Getting that backwards silently repoints every subsequent command at a
// different Hub.
func TestProfileAddSetsCurrentOnlyWhenThereIsNoneAndParsesTimeouts(t *testing.T) {
	t.Parallel()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	run := func(arguments ...string) (string, error) {
		var output bytes.Buffer
		root := NewRoot(Runtime{
			Stdout: &output, Stderr: &output,
			Getenv: func(name string) string {
				if name == "CONDUIT_CONFIG_FILE" {
					return configPath
				}
				return ""
			},
		})
		root.SetArgs(arguments)
		err := root.Execute()
		return output.String(), err
	}
	create := func(t *testing.T, name string, extra ...string) {
		t.Helper()
		arguments := append([]string{
			"--server", "https://hub.example.test",
			"--ca-file", "ca.pem", "--client-cert-file", "c.crt", "--client-key-file", "c.key",
		}, extra...)
		if _, err := run(append(arguments, "profile", "create", name)...); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	create(t, "first")
	create(t, "second")
	listed, err := run("profile", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed, `"current": "first"`) {
		t.Errorf("the second profile displaced the first as current:\n%s", listed)
	}

	// A profile timeout is a duration string, and an unparseable one has to be
	// refused when the profile is used rather than quietly falling back to the
	// default -- a command running on a timeout nobody chose is the failure
	// this prevents.
	if _, err := run("--server", "https://hub.example.test",
		"--ca-file", "ca.pem", "--client-cert-file", "c.crt", "--client-key-file", "c.key",
		"--timeout", "soon", "profile", "create", "bad-timeout"); err == nil {
		t.Error("an unparseable profile timeout was accepted")
	}
}

// The profile store is read before almost every command and written by the
// commands that change it. Both directions have error paths that nothing
// reached, and swallowing either leaves the CLI acting on a profile document
// it never successfully loaded, or reporting a profile it never stored.
func TestProfileStoreReportsReadAndWriteFailures(t *testing.T) {
	t.Parallel()

	run := func(configPath string, arguments ...string) (string, error) {
		var output bytes.Buffer
		root := NewRoot(Runtime{
			Stdout: &output, Stderr: &output,
			Getenv: func(name string) string {
				if name == "CONDUIT_CONFIG_FILE" {
					return configPath
				}
				return ""
			},
			ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) {
				return &fakeService{}, func() {}, nil
			},
		})
		root.SetArgs(arguments)
		err := root.Execute()
		return output.String(), err
	}

	t.Run("an unreadable store fails the command that needed it", func(t *testing.T) {
		t.Parallel()
		// No --profile is given, so the store is consulted for the current one
		// before the command connects. A document that cannot be parsed must
		// stop the command rather than be treated as "no profiles configured",
		// which would run against whatever the bare flags happen to say.
		configPath := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(configPath, []byte("version: [not a number\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := run(configPath, "--server", "https://hub.example.test",
			"--ca-file", "ca.pem", "--client-cert-file", "c.crt", "--client-key-file", "c.key",
			"changerequest", "list", "--environment", "dev")
		if err == nil {
			t.Error("a command ran with a profile store it could not read")
		}
	})

	t.Run("an unwritable store fails the command that changed it", func(t *testing.T) {
		t.Parallel()
		// The store is readable but its directory is not writable, so the read
		// succeeds and only the write fails. Reporting success here would tell
		// an operator a profile exists that was never persisted.
		//
		// The unwritable half is a directory mode, and CAP_DAC_OVERRIDE makes
		// root ignore it: as root the write succeeds and this fails for a
		// reason that has nothing to do with the code under test. CI runs
		// unprivileged so the coverage is kept there; skipping is honest,
		// whereas asserting something this process cannot make true is not.
		if os.Geteuid() == 0 {
			t.Skip("running as root: CAP_DAC_OVERRIDE defeats the unwritable-directory premise")
		}
		directory := t.TempDir()
		configPath := filepath.Join(directory, "config.yaml")
		if err := os.WriteFile(configPath, []byte("version: 1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })

		for _, command := range [][]string{{"profile", "create", "new"}, {"configure"}} {
			arguments := append([]string{
				"--server", "https://hub.example.test", "--ca-file", "ca.pem",
				"--client-cert-file", "c.crt", "--client-key-file", "c.key",
			}, command...)
			if _, err := run(configPath, arguments...); err == nil {
				t.Errorf("%v reported success writing to a read-only directory", command)
			}
		}
	})
}
