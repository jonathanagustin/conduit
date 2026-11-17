package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

const packageDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

func zarfArtifact() Artifact {
	return Artifact{
		Kind: ArtifactKindZarfPackage, Repository: "ghcr.io/example/excalidraw", Digest: packageDigest,
		Architectures: []string{"amd64"}, PackageName: "excalidraw",
	}
}

// A Release written before Kind existed carries none, and must stay a Helm
// chart with nothing new to answer for and nothing new in its document.
func TestAnArtifactWithNoKindIsAHelmChart(t *testing.T) {
	t.Parallel()
	chart := Artifact{Repository: "registry.example/api", Tag: "1.0.0", Digest: packageDigest}
	if chart.EffectiveKind() != ArtifactKindHelmChart || chart.IsPackage() {
		t.Fatalf("kind = %q package = %t", chart.EffectiveKind(), chart.IsPackage())
	}
	if problems := chart.PackageProblems(); len(problems) != 0 {
		t.Fatalf("a plain chart has package problems: %#v", problems)
	}
	if !chart.SupportsArchitecture("riscv64") {
		t.Fatal("a Helm chart is architecture-neutral")
	}
	encoded, err := json.Marshal(chart)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"repository":"registry.example/api","tag":"1.0.0","digest":"` + packageDigest + `"}`; string(encoded) != want {
		t.Fatalf("a chart's document changed, which would change every existing Release digest:\n got %s\nwant %s", encoded, want)
	}
}

func TestPackageArtifactIsAcceptedWhenPinnedAndDescribed(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{ArtifactKindZarfPackage, ArtifactKindUDSBundle} {
		artifact := zarfArtifact()
		artifact.Kind = kind
		if problems := artifact.PackageProblems(); len(problems) != 0 || !artifact.IsPackage() {
			t.Fatalf("%s: problems %#v", kind, problems)
		}
	}
}

func TestPackageArtifactRefusals(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		mutate func(*Artifact)
		field  string
	}{
		"tag-only reference":        {func(a *Artifact) { a.Digest, a.Tag = "", "1.0.0" }, "digest"},
		"tag beside the digest":     {func(a *Artifact) { a.Tag = "1.0.0" }, "tag"},
		"latest":                    {func(a *Artifact) { a.Tag = "latest" }, "tag"},
		"tag inside the repository": {func(a *Artifact) { a.Repository = "ghcr.io/example/excalidraw:1.0.0" }, "repository"},
		"digest inside repository":  {func(a *Artifact) { a.Repository += "@" + packageDigest }, "repository"},
		"uppercase digest":          {func(a *Artifact) { a.Digest = strings.ToUpper(a.Digest) }, "digest"},
		"no architectures":          {func(a *Artifact) { a.Architectures = nil }, "architectures"},
		"multiarchitecture index":   {func(a *Artifact) { a.Architectures = []string{"amd64", "arm64"} }, "architectures"},
		"unknown architecture":      {func(a *Artifact) { a.Architectures = []string{"sparc"} }, "architectures"},
		"repeated architecture":     {func(a *Artifact) { a.Architectures = []string{"amd64", "amd64"} }, "architectures"},
		"no package name":           {func(a *Artifact) { a.PackageName = "" }, "packageName"},
		"not a Zarf package name":   {func(a *Artifact) { a.PackageName = "Excalidraw_1" }, "packageName"},
		"unknown kind":              {func(a *Artifact) { a.Kind = "oci-thing" }, "kind"},
		"chart with architectures":  {func(a *Artifact) { a.Kind = ""; a.PackageName = "" }, "architectures"},
		"chart with a package name": {func(a *Artifact) { a.Kind = ArtifactKindHelmChart; a.Architectures = nil }, "packageName"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			artifact := zarfArtifact()
			testCase.mutate(&artifact)
			problems := artifact.PackageProblems()
			for _, problem := range problems {
				if problem.Field == testCase.field {
					return
				}
			}
			t.Fatalf("no %s problem reported: %#v", testCase.field, problems)
		})
	}
}

// A registry port is not a tag.
func TestRepositoryPortIsNotATag(t *testing.T) {
	t.Parallel()
	artifact := zarfArtifact()
	artifact.Repository = "oci://registry.example:5000/example/excalidraw"
	if problems := artifact.PackageProblems(); len(problems) != 0 {
		t.Fatalf("a registry port was read as a tag: %#v", problems)
	}
}

func TestPackageSupportsOnlyTheArchitecturesItWasBuiltFor(t *testing.T) {
	t.Parallel()
	artifact := zarfArtifact()
	artifact.Architectures = []string{"arm64"}
	if artifact.SupportsArchitecture("amd64") || !artifact.SupportsArchitecture("arm64") {
		t.Fatal("architecture support does not follow the declared list")
	}
}
