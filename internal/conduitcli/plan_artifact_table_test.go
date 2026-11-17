package conduitcli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestPlanStatusTableNamesTheArtifactKinds(t *testing.T) {
	t.Parallel()
	packaged := controlplane.Artifact{
		Kind: controlplane.ArtifactKindZarfPackage, Repository: "registry.example/packages/api",
		Digest:        "sha256:" + strings.Repeat("c", 64),
		Architectures: []string{"amd64", "arm64"}, PackageName: "api",
	}
	view := planStatusView{PlanRecord: controlplane.PlanRecord{ID: "pln_a", Plan: controlplane.Plan{
		Environment: "prod",
		Steps: []controlplane.PlanStep{
			{InstallationID: "a", Chart: packaged},
			{InstallationID: "b", Chart: controlplane.Artifact{Repository: "registry.example/web", Tag: "1.0.0"}},
			{InstallationID: "c", Chart: packaged},
		},
	}}}
	rows, err := view.MarshalCSV()
	if err != nil {
		t.Fatal(err)
	}
	header, row := rows[0], rows[1]
	if header[len(header)-1] != "artifacts" || row[len(row)-1] != "helm-chart zarf-package[amd64+arm64]" {
		t.Fatalf("table = %q", rows)
	}
	// The JSON body stays the Hub's document, with the whole artifact in it.
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"kind":"zarf-package"`, `"packageName":"api"`, `"architectures":["amd64","arm64"]`} {
		if !strings.Contains(string(body), fragment) {
			t.Fatalf("JSON body lacks %s: %s", fragment, body)
		}
	}
}
