package udsschema_test

import (
	"strings"
	"testing"

	devworkspace "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	packaging "github.com/jonathanagustin/conduit/internal/packaging/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
	"github.com/jonathanagustin/conduit/internal/udsschema"
)

// Every place in this repository that writes a UDS Core Package is held to the
// one pinned schema here. The Gitless renderer is checked in its own package,
// where it also refuses to return an invalid document; the application
// generator is checked through the packaging gate its output passes.
func TestEveryPackageEmitterProducesASchemaValidPackage(t *testing.T) {
	t.Parallel()
	emitters := map[string]func(*testing.T) []byte{
		"devworkspace egress": func(t *testing.T) []byte {
			rendered, err := devworkspace.RenderWorkspacePackageCR("workspaces", "example", devworkspace.EgressConfig{
				GitHost: "git.example.internal", Registry: "registry.example.internal",
			})
			if err != nil {
				t.Fatal(err)
			}
			return rendered
		},
		"packaging scaffold": func(t *testing.T) []byte {
			scaffold, err := packaging.GenerateScaffold(packaging.ScaffoldInput{
				Profile: packaging.ScaffoldProfile, Name: "sample-app", Version: "0.1.0",
				SourceRevision: strings.Repeat("a", 40), SourceSHA256: strings.Repeat("b", 64),
				Image: "registry.example.invalid/app@sha256:" + strings.Repeat("c", 64),
				Port:  8080, UID: 1000, CPUMilli: 100, MemoryMiB: 128,
				ZarfVersion: packaging.ZarfVersion, UDSCommit: packaging.UDSCommit,
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range scaffold.Files {
				if file.Path == "charts/uds-config/templates/package.yaml" {
					return []byte(file.Content)
				}
			}
			t.Fatal("the scaffold has no Package document")
			return nil
		},
		"packaging workflow fixture, declared needs": func(t *testing.T) []byte {
			return renderFixture(t, "name: static-web\nkind: fixture\nport: 8080\nversion: 0.1.0\n")
		},
	}
	for name, emit := range emitters {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rendered := emit(t)
			if violations := udsschema.ValidatePackage(rendered); len(violations) != 0 {
				t.Fatalf("%v\n%s", violations, rendered)
			}
		})
	}
}

func renderFixture(t *testing.T, source string) []byte {
	t.Helper()
	files, err := fixtures.Render([]byte(source), "sha256:"+strings.Repeat("a", 64), "template-1", "zarf=v0.82.0")
	if err != nil {
		t.Fatal(err)
	}
	return files.CR
}
