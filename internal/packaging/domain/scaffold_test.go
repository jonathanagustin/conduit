package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func scaffoldFixture() ScaffoldInput {
	return ScaffoldInput{Profile: ScaffoldProfile, Name: "sample-app", Version: "0.1.0", SourceRevision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64), Image: "registry.example.invalid/app@sha256:" + strings.Repeat("c", 64), Port: 8080, UID: 1000, CPUMilli: 100, MemoryMiB: 128, ZarfVersion: ZarfVersion, UDSCommit: UDSCommit}
}
func TestScaffoldDeterminismAndProvenance(t *testing.T) {
	in := scaffoldFixture()
	a, e := GenerateScaffold(in)
	if e != nil {
		t.Fatal(e)
	}
	b, e := GenerateScaffold(in)
	if e != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("not deterministic")
	}
	if a.Status != "scaffold-needs-validation" || len(a.Files) != 9 || len(a.Digest) != 64 {
		t.Fatalf("invalid envelope %#v", a)
	}
	for _, change := range []func(*ScaffoldInput){func(i *ScaffoldInput) { i.SourceRevision = strings.Repeat("d", 40) }, func(i *ScaffoldInput) { i.SourceSHA256 = strings.Repeat("e", 64) }, func(i *ScaffoldInput) { i.Port = 8081 }} {
		other := in
		change(&other)
		got, e := GenerateScaffold(other)
		if e != nil || got.Digest == a.Digest {
			t.Fatal("proposal changed without digest change")
		}
	}
	for n, f := range a.Files {
		if n > 0 && a.Files[n-1].Path >= f.Path {
			t.Fatal("unstable file order")
		}
		var v map[string]any
		if e := json.Unmarshal([]byte(f.Content), &v); e != nil {
			t.Fatal(e)
		}
		switch f.Path {
		case "zarf.yaml":
			c := v["components"].([]any)[0].(map[string]any)
			if len(c["images"].([]any)) != 1 || len(c["charts"].([]any)) != 2 || c["actions"] != nil {
				t.Fatal("incorrect packaging contract")
			}
		case "charts/uds-config/templates/package.yaml":
			net := v["spec"].(map[string]any)["network"].(map[string]any)
			if len(net["allow"].([]any)) != 0 || net["expose"] != nil {
				t.Fatal("unexpected network access")
			}
		case "charts/application/templates/deployment.yaml":
			spec := v["spec"].(map[string]any)
			pod := spec["template"].(map[string]any)["spec"].(map[string]any)
			if spec["replicas"] != float64(1) || pod["automountServiceAccountToken"] != false || pod["volumes"] != nil {
				t.Fatal("unsupported workload scope")
			}
		}
	}
}
func TestScaffoldFailsClosed(t *testing.T) {
	for _, change := range []func(*ScaffoldInput){func(i *ScaffoldInput) { i.Profile = "geoserver-stateful" }, func(i *ScaffoldInput) { i.Image = "app:latest" }, func(i *ScaffoldInput) { i.Name = "../escape" }, func(i *ScaffoldInput) { i.Name = "kube-system" }, func(i *ScaffoldInput) { i.UID = 0 }, func(i *ScaffoldInput) { i.Port = 80 }, func(i *ScaffoldInput) { i.CPUMilli = 0 }, func(i *ScaffoldInput) { i.MemoryMiB = 0 }, func(i *ScaffoldInput) { i.SourceRevision = "main" }, func(i *ScaffoldInput) { i.SourceSHA256 = "" }, func(i *ScaffoldInput) { i.ZarfVersion = "latest" }, func(i *ScaffoldInput) { i.ZarfVersion = "1.0.0" }, func(i *ScaffoldInput) { i.UDSCommit = "main" }, func(i *ScaffoldInput) { i.Version = "{{ .Values.secret }}" }} {
		in := scaffoldFixture()
		change(&in)
		if _, e := GenerateScaffold(in); e == nil {
			t.Fatalf("accepted unsupported input %#v", in)
		}
	}
}

func TestScaffoldRejectsMalformedImageAndVersion(t *testing.T) {
	for _, image := range []string{"registry//app", "registry/app/", "a..b/app"} {
		in := scaffoldFixture()
		in.Image = image + "@sha256:" + strings.Repeat("a", 64)
		if _, err := GenerateScaffold(in); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
	for _, version := range []string{"999999999999999999999999.0.0", "01.0.0"} {
		in := scaffoldFixture()
		in.Version = version
		if _, err := GenerateScaffold(in); err == nil {
			t.Fatal("invalid version accepted")
		}
	}
}
