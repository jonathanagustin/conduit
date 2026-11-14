package schema_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/jonathanagustin/conduit/internal/apollographql/domain"
	"github.com/jonathanagustin/conduit/internal/apollographql/schema"
)

// The embedded copy exists only because Go cannot embed across package
// boundaries. If it ever differs from the artifact the provenance gate governs,
// the server would answer for an interface nobody verified.
func TestEmbeddedSchemaMatchesTheGovernedArtifact(t *testing.T) {
	canonical, err := os.ReadFile(filepath.Join("..", "..", "..", schema.Name))
	if err != nil {
		t.Fatalf("read %s: %v", schema.Name, err)
	}
	if string(canonical) != schema.SDL() {
		t.Fatalf(
			"the embedded interface differs from %s; re-run "+
				"scripts/apollo-graphql/build-interface-schema.mjs and copy its output into this package",
			schema.Name,
		)
	}
}

// The JS gate and the Go server read the same SDL, and both count it. If their
// counts ever disagreed, one of the two published numbers would be wrong and
// nothing would say which -- so they are compared rather than trusted.
func TestTheProvenanceReportCountsWhatTheServerReads(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs/reference-platform/observed/reference-interface-provenance.json"))
	if err != nil {
		t.Fatalf("read the provenance report: %v", err)
	}
	var report struct {
		ObservedFields      int `json:"observedFields"`
		DocumentedFields    int `json:"documentedFields"`
		ConduitNativeFields int `json:"conduitNativeFields"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("parse the provenance report: %v", err)
	}
	declared, err := domain.ReadProvenance(schema.SDL())
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	observed, documented, native := declared.Count()
	if observed != report.ObservedFields || documented != report.DocumentedFields || native != report.ConduitNativeFields {
		t.Fatalf(
			"the served interface carries %d observed / %d documented / %d Conduit-native field(s); the "+
				"provenance report claims %d / %d / %d. Re-run "+
				"scripts/apollo-graphql/build-interface-schema.mjs",
			observed, documented, native,
			report.ObservedFields, report.DocumentedFields, report.ConduitNativeFields,
		)
	}
}

// Every served field now traces to some Apollo source, which is a stronger claim
// than the interface started with and an easy one to lose quietly. It is pinned
// so that adding a field with no citation fails here rather than passing as a
// rounding change in a report nobody reads.
//
// This is not a demand that the number stay zero. If Conduit genuinely invents a
// coordinate, the honest move is to declare it CONDUIT_NATIVE and update this
// count deliberately -- which is exactly what failing here forces.
func TestNoServedFieldIsWithoutAnApolloSource(t *testing.T) {
	declared, err := domain.ReadProvenance(schema.SDL())
	if err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	if _, _, native := declared.Count(); native != 0 {
		t.Errorf(
			"%d served field(s) carry no Apollo source of any kind; the pinned count is 0. If Conduit "+
				"invented a coordinate on purpose, update this count and docs/reference-platform/conformance.md",
			native,
		)
	}
}

func TestSchemaLoads(t *testing.T) {
	loaded, err := gqlparser.LoadSchema(&ast.Source{Name: schema.Name, Input: schema.SDL()})
	if err != nil {
		t.Fatalf("the served interface is not a loadable GraphQL schema: %v", err)
	}
	if loaded.Query == nil {
		t.Fatal("the served interface declares no query root, so nothing can be asked of it")
	}
}
