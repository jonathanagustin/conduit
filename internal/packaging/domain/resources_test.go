package domain

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func resourcesFixture() ScaffoldInput {
	in := scaffoldFixture()
	in.Profile = ExistingResourcesProfile
	in.ExistingClaim = &ExistingClaim{Name: "sample-data", MountPath: "/opt/geoserver_data"}
	in.ExistingSecretFile = &ExistingSecretFile{Name: "sample-admin", Key: "password", Env: "GEOSERVER_ADMIN_PASSWORD_FILE"}
	return in
}
func TestExistingResourceScaffoldReferencesOnly(t *testing.T) {
	in := resourcesFixture()
	out, err := GenerateScaffold(in)
	if err != nil {
		t.Fatal(err)
	}
	same, _ := GenerateScaffold(in)
	if !reflect.DeepEqual(out, same) {
		t.Fatal("nondeterministic")
	}
	if len(out.Files) != 9 || out.Status != "scaffold-needs-validation" {
		t.Fatal("unexpected envelope")
	}
	for _, f := range out.Files {
		var obj map[string]any
		if err := json.Unmarshal([]byte(f.Content), &obj); err != nil {
			t.Fatal(err)
		}
		if obj["kind"] == "Secret" || obj["kind"] == "PersistentVolumeClaim" {
			t.Fatal("created external resource")
		}
		if f.Path != "charts/application/templates/deployment.yaml" {
			continue
		}
		spec := obj["spec"].(map[string]any)
		if spec["strategy"].(map[string]any)["type"] != "Recreate" || spec["replicas"] != float64(1) {
			t.Fatal("unsafe rollout shape")
		}
		pod := spec["template"].(map[string]any)["spec"].(map[string]any)
		if pod["initContainers"] != nil || pod["securityContext"].(map[string]any)["fsGroup"] != nil || pod["automountServiceAccountToken"] != false {
			t.Fatal("permission workaround")
		}
		volumes := pod["volumes"].([]any)
		if len(volumes) != 2 {
			t.Fatal("volume count")
		}
		claim := volumes[0].(map[string]any)["persistentVolumeClaim"].(map[string]any)
		if claim["claimName"] != "sample-data" {
			t.Fatal("wrong claim")
		}
		secret := volumes[1].(map[string]any)["secret"].(map[string]any)
		items := secret["items"].([]any)
		if secret["secretName"] != "sample-admin" || secret["optional"] != false || secret["defaultMode"] != float64(292) || len(items) != 1 || items[0].(map[string]any)["key"] != "password" || items[0].(map[string]any)["path"] != "value" {
			t.Fatal("whole-secret or incorrect projection")
		}
		c := pod["containers"].([]any)[0].(map[string]any)
		mounts := c["volumeMounts"].([]any)
		if mounts[1].(map[string]any)["readOnly"] != true || mounts[1].(map[string]any)["subPath"] != nil {
			t.Fatal("unsafe secret mount")
		}
		env := c["env"].([]any)[0].(map[string]any)
		if env["value"] != "/run/secrets/conduit/value" || env["name"] != in.ExistingSecretFile.Env {
			t.Fatal("arbitrary env")
		}
	}
	changed := resourcesFixture()
	changed.ExistingSecretFile.Key = "other"
	other, _ := GenerateScaffold(changed)
	if other.Digest == out.Digest {
		t.Fatal("secret reference unbound")
	}
	changed = resourcesFixture()
	changed.ExistingClaim.Name = "other-data"
	other, _ = GenerateScaffold(changed)
	if other.Digest == out.Digest {
		t.Fatal("claim reference unbound")
	}
}
func TestExistingResourceInputsFailClosed(t *testing.T) {
	for _, change := range []func(*ScaffoldInput){
		func(i *ScaffoldInput) { i.Profile = ScaffoldProfile },
		func(i *ScaffoldInput) { i.ExistingClaim = nil; i.ExistingSecretFile = nil },
		func(i *ScaffoldInput) { i.ExistingClaim.Name = "other/claim" },
		func(i *ScaffoldInput) { i.ExistingSecretFile.Name = "../secret" },
		func(i *ScaffoldInput) { i.ExistingSecretFile.Key = "../key" },
		func(i *ScaffoldInput) { i.ExistingSecretFile.Env = "LD_PRELOAD" },
		func(i *ScaffoldInput) { i.ExistingSecretFile.Env = "lower_FILE" },
	} {
		in := resourcesFixture()
		change(&in)
		if _, err := GenerateScaffold(in); err == nil {
			t.Fatal("unsafe reference accepted")
		}
	}
	for _, path := range []string{"/", "/opt", "/opt/../etc", "/opt//data", "/opt/data/", "/run/secrets/conduit", "/var/lib/data/child", "C:\\data", "/opt/{{bad}}", "/opt/data\n"} {
		in := resourcesFixture()
		in.ExistingClaim.MountPath = path
		if _, err := GenerateScaffold(in); err == nil {
			t.Fatal("unsafe mount accepted", path)
		}
	}
	for _, only := range []string{"claim", "secret"} {
		in := resourcesFixture()
		if only == "claim" {
			in.ExistingSecretFile = nil
		} else {
			in.ExistingClaim = nil
		}
		if _, err := GenerateScaffold(in); err != nil {
			t.Fatal(err)
		}
	}
}
func TestReviewedPlanRejectsResourceProfileDespiteNoneDecisions(t *testing.T) {
	revision, docs, in := planFixture(t)
	in.Scaffold.Profile = ExistingResourcesProfile
	in.Scaffold.ExistingClaim = &ExistingClaim{Name: "sample-data", MountPath: "/opt/geoserver_data"}
	out, err := PlanPackaging(revision, "Dockerfile", docs, in)
	if err != nil || out.Artifacts != nil || out.PlanSHA256 != "" || !strings.Contains(strings.Join(out.Blockers, " "), "resource lifecycle") {
		t.Fatal("references bypassed review boundary", err)
	}
}
