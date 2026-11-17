package controlplane

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
)

const packageArtifactDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

// zarfFoundation is foundation() with the dependency-free Release shipping a
// Zarf package instead of a chart.
func zarfFoundation() DesiredState {
	state := foundation()
	state.Environment.PackageArchitecture = "amd64"
	state.Releases[0].Chart = Artifact{
		Kind: ArtifactKindZarfPackage, Repository: "registry.example/packages/example-api", Digest: packageArtifactDigest,
		Architectures: []string{"amd64"}, PackageName: "example-api",
	}
	return state
}

func problemAt(errs []ValidationError, path string) (string, bool) {
	for _, validation := range errs {
		if validation.Path == path {
			return validation.Message, true
		}
	}
	return "", false
}

func TestReleaseMayDeclareAZarfPackageOrUDSBundle(t *testing.T) {
	for _, kind := range []string{ArtifactKindZarfPackage, ArtifactKindUDSBundle} {
		state := zarfFoundation()
		state.Releases[0].Chart.Kind = kind
		if errs := Validate(state); len(errs) != 0 {
			t.Fatalf("%s Release refused: %#v", kind, errs)
		}
	}
}

func TestPackageReleaseRefusesAMutableReference(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Artifact)
		path   string
	}{
		"tag-only":              {func(a *Artifact) { a.Digest, a.Tag = "", "0.1.0" }, "releases[0].chart.digest"},
		"tag beside the digest": {func(a *Artifact) { a.Tag = "0.1.0" }, "releases[0].chart.tag"},
		"tag in the repository": {func(a *Artifact) { a.Repository += ":0.1.0" }, "releases[0].chart.repository"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			state := zarfFoundation()
			testCase.mutate(&state.Releases[0].Chart)
			message, found := problemAt(Validate(state), testCase.path)
			if !found {
				t.Fatalf("a %s reference was accepted: %#v", name, Validate(state))
			}
			if testCase.path != "releases[0].chart.repository" && !strings.Contains(message, "mutable") {
				t.Fatalf("the refusal does not say why: %q", message)
			}
			if _, err := BuildPlan(state); err == nil {
				t.Fatal("a Plan was built from a Release with a mutable package reference")
			}
		})
	}
}

func TestPackageReleaseRefusesWhatItCannotCarry(t *testing.T) {
	cases := map[string]struct {
		mutate func(*DesiredState)
		path   string
	}{
		"no architectures": {func(s *DesiredState) { s.Releases[0].Chart.Architectures = nil }, "releases[0].chart.architectures"},
		"no package name":  {func(s *DesiredState) { s.Releases[0].Chart.PackageName = "" }, "releases[0].chart.packageName"},
		"unknown kind":     {func(s *DesiredState) { s.Releases[0].Chart.Kind = "oci-thing" }, "releases[0].chart.kind"},
		"kind on an image": {func(s *DesiredState) { s.Releases[0].Images[0].Kind = ArtifactKindZarfPackage }, "releases[0].images[0].kind"},
		"a Helm rollout strategy": {
			func(s *DesiredState) { s.Releases[0].RolloutStrategy = RolloutApplyChangesNoWait }, "releases[0].rolloutStrategy",
		},
		"architectures on a chart": {
			func(s *DesiredState) { s.Releases[1].Chart.Architectures = []string{"amd64"} }, "releases[1].chart.architectures",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			state := zarfFoundation()
			testCase.mutate(&state)
			if _, found := problemAt(Validate(state), testCase.path); !found {
				t.Fatalf("accepted: %#v", Validate(state))
			}
		})
	}
}

// The step is the only thing the Spoke sees, so the artifact's kind, pin,
// architectures and package name must reach it intact, and the Environment's
// Helm defaults must not.
func TestPlanCarriesThePackageArtifactOnItsStep(t *testing.T) {
	state := zarfFoundation()
	state.Environment.HelmExecution = &HelmExecutionDefaults{DefaultRolloutStrategy: RolloutApplyChangesNoWait}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	var packaged, charted *PlanStep
	for index := range plan.Steps {
		if plan.Steps[index].Product == "app.example:example-api" {
			packaged = &plan.Steps[index]
		} else {
			charted = &plan.Steps[index]
		}
	}
	if packaged == nil || charted == nil {
		t.Fatalf("steps = %#v", plan.Steps)
	}
	artifact := packaged.Chart
	if artifact.Kind != ArtifactKindZarfPackage || artifact.Digest != packageArtifactDigest || artifact.Tag != "" ||
		artifact.PackageName != "example-api" || len(artifact.Architectures) != 1 || artifact.Architectures[0] != "amd64" {
		t.Fatalf("step artifact = %#v", artifact)
	}
	if packaged.HelmExecution != (HelmExecutionPolicy{RolloutStrategy: RolloutManageRollout}) {
		t.Fatalf("a package step inherited Helm execution defaults: %#v", packaged.HelmExecution)
	}
	if charted.Chart.IsPackage() || charted.HelmExecution.RolloutStrategy != RolloutApplyChangesNoWait {
		t.Fatalf("the Helm step changed: %#v", charted)
	}
	encoded, err := json.Marshal(packaged)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"kind":"zarf-package"`, `"architectures":["amd64"]`, `"packageName":"example-api"`} {
		if !strings.Contains(string(encoded), fragment) {
			t.Fatalf("the step document lacks %s: %s", fragment, encoded)
		}
	}
}

func TestPackagePlanningRequiresTheDeclaredTargetArchitecture(t *testing.T) {
	for _, kind := range []string{ArtifactKindZarfPackage, ArtifactKindUDSBundle} {
		for _, architecture := range []string{"", "arm64"} {
			t.Run(kind+"/"+architecture, func(t *testing.T) {
				state := zarfFoundation()
				state.Environment.PackageArchitecture = architecture
				state.Releases[0].Chart.Kind = kind
				message, found := problemAt(Validate(state), "entities[1].release.chart.architectures")
				if !found || !strings.Contains(message, "packageArchitecture") {
					t.Fatalf("missing target architecture refusal: %#v", Validate(state))
				}
				if _, err := BuildPlan(state); err == nil {
					t.Fatal("a package Plan was issued without a compatible declared target")
				}
			})
		}
	}
	state := foundation()
	if _, err := BuildPlan(state); err != nil {
		t.Fatalf("Helm-only Plan now requires a package architecture: %v", err)
	}
	state.Environment.PackageArchitecture = "sparc"
	if _, found := problemAt(Validate(state), "environment.packageArchitecture"); !found {
		t.Fatal("an unsupported declared architecture was accepted")
	}
	// Unselected packages do not impose a target on a Helm-only deployment.
	state = foundation()
	packaged := zarfFoundation().Releases[0]
	packaged.Version = "0.2.0"
	state.Releases = append(state.Releases, packaged)
	if _, err := BuildPlan(state); err != nil {
		t.Fatalf("an unused package blocked a Helm installation: %v", err)
	}
}

func TestPackageChannelSelectsACompatibleArchitecture(t *testing.T) {
	state := channelFoundation("0.1.0", "0.2.0")
	state.Environment.PackageArchitecture = "amd64"
	for index := range state.Releases {
		state.Releases[index].Chart = zarfFoundation().Releases[0].Chart
	}
	state.Releases[1].Chart.Architectures = []string{"arm64"}
	plan, err := BuildPlan(state)
	if err != nil || len(plan.Steps) != 1 || plan.Steps[0].Version != "0.1.0" {
		t.Fatalf("channel did not select the compatible package: %#v, %v", plan, err)
	}
	state.Environment.PackageArchitecture = "arm64"
	plan, err = BuildPlan(state)
	if err != nil || len(plan.Steps) != 1 || plan.Steps[0].Version != "0.2.0" {
		t.Fatalf("channel ignored the changed target architecture: %#v, %v", plan, err)
	}
	state.Environment.PackageArchitecture = ""
	if _, err = BuildPlan(state); err == nil || !strings.Contains(err.Error(), "packageArchitecture") {
		t.Fatalf("missing target lost its refusal reason: %v", err)
	}
}

func TestPackagePlanningRefusesAMultiarchitectureDeclaration(t *testing.T) {
	state := zarfFoundation()
	state.Releases[0].Chart.Architectures = []string{"amd64", "arm64"}
	if _, found := problemAt(Validate(state), "releases[0].chart.architectures"); !found {
		t.Fatal("multiarchitecture declaration was accepted without platform resolution")
	}
	if _, err := BuildPlan(state); err == nil {
		t.Fatal("a Plan was issued for an unsupported multiarchitecture declaration")
	}
}

// Adding the fields must not move the digest of any Release that does not use
// them: a Plan approved against the old digest would stop matching.
func TestHelmReleaseDigestIsUnchangedByThePackageFields(t *testing.T) {
	release := foundation().Releases[0]
	got, err := releaseContentDigest(release)
	if err != nil {
		t.Fatal(err)
	}
	// The exact bytes the digest covered before Artifact carried a kind: three
	// fields per artifact, in this order, and nothing else.
	artifact := `{"repository":"registry.example/example-api","tag":"0.1.0","digest":"` + digest + `"}`
	want := sha256Digest(sha256.Sum256([]byte(
		`{"product":"app.example:example-api","version":"0.1.0","chart":` + artifact + `,"images":[` + artifact + `]}`)))
	if got != want {
		t.Fatalf("Helm Release digest = %s, want %s", got, want)
	}
	packaged := zarfFoundation().Releases[0]
	changed, err := releaseContentDigest(packaged)
	if err != nil || changed == got {
		t.Fatalf("the artifact kind is not part of the Release digest: %s, %v", changed, err)
	}
}
