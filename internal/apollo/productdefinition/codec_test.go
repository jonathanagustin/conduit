package productdefinition

import (
	"encoding/json"
	"errors"
	"go.yaml.in/yaml/v4"
	"io"
	"reflect"
	"strings"
	"testing"
)

const codecServiceManifest = "product-group: com.example\nproduct-name: demo\nproduct-version: 1.0.0\nproduct-type: service.v1\n"

type codecUnreadable struct{ calls int }

func (r *codecUnreadable) Read([]byte) (int, error) {
	r.calls++
	return 0, errors.New("configuration read after manifest refusal")
}

func TestDefinitionCodecRetainsOptionalConfigurationAndSerializedCarrier(t *testing.T) {
	for _, configuration := range []io.Reader{nil, strings.NewReader("")} {
		definition, problems, err := Decode(strings.NewReader(codecServiceManifest), configuration, nil)
		if err != nil || len(problems) != 0 {
			t.Fatalf("optional configuration: %v %v", err, problems)
		}
		if definition.Manifest.ProductType != "service.v1" || definition.Templates != nil {
			t.Fatalf("carrier=%+v", definition)
		}
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != 2 || fields["manifest"] == nil || fields["configuration"] == nil {
			t.Fatalf("JSON carrier changed: %s", encoded)
		}
	}
	templates := []string{"settings.tmpl"}
	definition, problems, err := Decode(strings.NewReader(codecServiceManifest), strings.NewReader("unmodelled-setting: {enabled: false}\n"), templates)
	if err != nil || len(problems) != 0 || !reflect.DeepEqual(definition.Templates, templates) {
		t.Fatalf("template/unknown input: %+v %v %v", definition, problems, err)
	}
	encoded, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"templates":["settings.tmpl"]`) || strings.Contains(string(encoded), `"unmodelled-setting"`) {
		t.Fatalf("raw document lost: %s", encoded)
	}
	yamlBody, err := yaml.Marshal(definition.Configuration)
	if err != nil || !strings.Contains(string(yamlBody), "unmodelled-setting:") || !strings.Contains(string(yamlBody), "enabled: false") {
		t.Fatalf("unknown YAML input lost: %s %v", yamlBody, err)
	}
}

func TestDefinitionCodecKeepsStructuralErrorsSeparateFromPairProblems(t *testing.T) {
	unreadable := &codecUnreadable{}
	if _, problems, err := Decode(strings.NewReader("product-group: [\n"), unreadable, nil); err == nil || problems != nil || unreadable.calls != 0 {
		t.Fatalf("manifest refusal/order: %v %v reads%d", err, problems, unreadable.calls)
	}
	for _, tc := range []struct {
		manifest, configuration string
		diagnostic              string
	}{
		{codecServiceManifest + "---\nproduct-name: second\n", "", "manifest must contain exactly one YAML document"},
		{codecServiceManifest, "resources: [\n", "decode Apollo Product configuration:"},
		{codecServiceManifest, "{}\n---\n{}\n", "configuration must contain exactly one YAML document"},
	} {
		_, problems, err := Decode(strings.NewReader(tc.manifest), strings.NewReader(tc.configuration), nil)
		if err == nil || problems != nil || !strings.Contains(err.Error(), tc.diagnostic) {
			t.Fatalf("structural result: %v %v", err, problems)
		}
	}
	definition, problems, err := Decode(strings.NewReader(strings.Replace(codecServiceManifest, "com.example", "invalid group", 1)), nil, []string{})
	if err != nil || len(problems) == 0 || problems[0].Path != "manifest.product" || definition.Templates == nil {
		t.Fatalf("validation/presence result: %+v %v %v", definition, problems, err)
	}
}
