package productconfiguration

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestOptionalConfigurationAndStructuralFailures(t *testing.T) {
	for _, r := range []io.Reader{nil, strings.NewReader(""), strings.NewReader("# comment\n"), strings.NewReader("#" + strings.Repeat("x", 1<<20))} {
		c, err := Decode(r)
		if err != nil || !reflect.DeepEqual(c, Configuration{}) {
			t.Fatalf("optional document: %#v %v", c, err)
		}
	}
	for _, input := range []string{"conf: [unfinished", "conf: {}\n---\nconf: {}\n", "conf: {}\n---\n[unfinished", "resources:\n  requests:\n    cpu: 1\n    cpu: 2\n"} {
		if _, err := Decode(strings.NewReader(input)); err == nil {
			t.Fatalf("invalid document accepted: %q", input)
		}
	}
	cause := errors.New("input failed")
	if _, err := Decode(decoderErrorReader{cause}); !errors.Is(err, cause) {
		t.Fatalf("reader cause lost: %v", err)
	}
}

type decoderErrorReader struct{ cause error }

func (r decoderErrorReader) Read([]byte) (int, error) { return 0, r.cause }

func TestConfigurationPresenceTagsUnknownNodesAndManagedFiles(t *testing.T) {
	c, err := Decode(strings.NewReader(`resources:
  requests:
    cpu: 8
    memory: 10Gi
replication:
  desired: 0
endpoints:
  definitions:
    web:
      desired-port: 8443
managed_files:
  settings.yml: server
  expanded.json:
    type: json
    reload-type: runtime
    content:
      enabled: false
discovery:
  produces:
    service:
      port: 8443
      vendor-field: [one, two]
vendor-config:
  nested: [three, four]
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Resources.Requests.CPU != "8" || c.Resources.Requests.Memory != "10Gi" || c.Replication.Desired == nil || *c.Replication.Desired != 0 {
		t.Fatalf("presence/quantities: %#v", c)
	}
	shorthand := c.ManagedFiles["settings.yml"]
	if !shorthand.Shorthand || shorthand.Content != "server" || shorthand.ReloadType != ReloadTypeInstall || shorthand.Type != "" {
		t.Fatalf("shorthand: %#v", shorthand)
	}
	expanded := c.ManagedFiles["expanded.json"]
	if expanded.Shorthand || expanded.Type != "json" || expanded.ReloadType != "runtime" || expanded.Content.(map[string]any)["enabled"] != false {
		t.Fatalf("expanded: %#v", expanded)
	}
	if c.Endpoints.Definitions["web"].EffectivePath() != "/" || (EndpointDefinition{Path: "/app"}).EffectivePath() != "/app" {
		t.Fatal("endpoint path changed")
	}
	checkUnknown := func(c Configuration) {
		t.Helper()
		n := c.Other["vendor-config"]
		if n.Kind != yaml.MappingNode || n.Content[1].Content[1].Value != "four" {
			t.Fatal("unknown root node changed")
		}
		p := c.Discovery.Produces["service"].Payload["vendor-field"]
		if p.Kind != yaml.SequenceNode || p.Content[1].Value != "two" {
			t.Fatal("open discovery payload changed")
		}
	}
	checkUnknown(c)
	encoded, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	checkUnknown(again)
	if again.Replication.Desired == nil || *again.Replication.Desired != 0 {
		t.Fatal("explicit zero lost")
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`"desired":0`, `"managedFiles"`, `"reloadType":"install"`, `"shorthand":true`, `"desiredPort":8443`} {
		if !strings.Contains(string(b), part) {
			t.Fatalf("JSON tag/presence %s: %s", part, b)
		}
	}
	if strings.Contains(string(b), "vendor-config") || strings.Contains(string(b), "vendor-field") {
		t.Fatal("YAML-only unknown nodes leaked into JSON")
	}
	absent, err := Decode(strings.NewReader("replication: {}\n"))
	if err != nil || absent.Replication == nil || absent.Replication.Desired != nil {
		t.Fatalf("absent desired changed: %#v %v", absent, err)
	}
}

func TestConfigurationStrictNestedFieldsAndLegacyTypeLabels(t *testing.T) {
	for _, tt := range []struct{ input, label string }{
		{"[wrong]\n", "into controlplane.ApolloProductConfiguration"},
		{"resources:\n  unknown: true\n", "field unknown not found in type controlplane.ApolloResourceRequirements"},
		{"resources:\n  requests:\n    unknown: true\n", "field unknown not found in type controlplane.ApolloResourceQuantities"},
		{"replication:\n  unknown: true\n", "field unknown not found in type controlplane.ApolloReplication"},
		{"volumes: wrong\n", "into map[string]controlplane.ApolloVolumeConfiguration"},
		{"endpoints:\n  definitions:\n    web:\n      unknown: true\n", "field unknown not found in type controlplane.ApolloEndpointDefinition"},
		{"generated_secrets:\n  token:\n    unknown: true\n", "field unknown not found in type controlplane.ApolloGeneratedSecret"},
	} {
		_, err := Decode(strings.NewReader(tt.input))
		var typed *yaml.LoadErrors
		var detail *yaml.LoadError
		if err == nil || !errors.As(err, &typed) || !errors.As(err, &detail) || detail.Mark.Line < 1 || !strings.HasPrefix(err.Error(), "decode Apollo Product configuration: yaml: construct errors: ") || !strings.Contains(err.Error(), tt.label) {
			t.Fatalf("input %q: %v", tt.input, err)
		}
	}
	for _, tt := range []struct{ input, want string }{
		{"managed_files:\n  file.yml:\n    type: yaml\n    reload_type: install\n", "field reload_type not found in managed file"},
		{"managed_files:\n  file.yml: [wrong]\n", "managed file must be a mapping or a conf key"},
	} {
		if _, err := Decode(strings.NewReader(tt.input)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("managed-file strictness: %v", err)
		}
	}
}
