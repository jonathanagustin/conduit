package domain_test

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollographql/domain"
)

const wellFormed = `directive @provenance(tier: ProvenanceTier!) on FIELD_DEFINITION

type Query {
  apollo: ApolloQuery @provenance(tier: OBSERVED)
}

type ApolloPlan {
  rid: String @provenance(tier: CONDUIT_NATIVE)
  environment: String @provenance(tier: CONDUIT_NATIVE)
}
`

func TestReadProvenanceReadsEveryTier(t *testing.T) {
	declared, err := domain.ReadProvenance(wellFormed)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	observed, native := declared.Count()
	if observed != 1 || native != 2 {
		t.Fatalf("counted %d observed / %d native", observed, native)
	}
	tier, found := declared.TierOf(domain.Coordinate{Type: "ApolloPlan", Field: "rid"})
	if !found || tier != domain.ConduitNative {
		t.Fatalf("ApolloPlan.rid read as %q (found=%v)", tier, found)
	}
}

// The refusal that makes the annotation load-bearing. A field with no tier is
// the one whose provenance nobody can state, so a tolerant read would let
// exactly the dangerous case through.
func TestAFieldWithNoProvenanceIsRefused(t *testing.T) {
	_, err := domain.ReadProvenance(strings.Replace(
		wellFormed, "  rid: String @provenance(tier: CONDUIT_NATIVE)", "  rid: String", 1,
	))
	if err == nil {
		t.Fatal("a field with no provenance annotation was accepted")
	}
	if !strings.Contains(err.Error(), "rid") {
		t.Errorf("the refusal does not name the field: %v", err)
	}
}

func TestAnUnknownTierIsRefused(t *testing.T) {
	_, err := domain.ReadProvenance(strings.Replace(wellFormed, "OBSERVED", "GUESSED", 1))
	if err == nil {
		t.Fatal("an unknown provenance tier was accepted")
	}
}

func TestSomethingThatIsNotTheServedInterfaceIsRefused(t *testing.T) {
	if _, err := domain.ReadProvenance("type Query {\n}\n"); err == nil {
		t.Fatal("a schema with no annotated field was accepted as the served interface")
	}
}

// --- reconciliation ---------------------------------------------------------

func TestAnInventedCoordinateNothingServesIsADefect(t *testing.T) {
	declared, err := domain.ReadProvenance(wellFormed)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	coverage := domain.NewCoverage(map[domain.Coordinate]string{
		{Type: "Query", Field: "apollo"}:           "root",
		{Type: "ApolloPlan", Field: "rid"}:         "reporting",
		{Type: "ApolloPlan", Field: "environment"}: "reporting",
	})
	if defects := domain.Defects(domain.Reconcile(declared, coverage)); len(defects) != 0 {
		t.Fatalf("a fully covered interface reported defects: %v", defects)
	}

	partial := domain.NewCoverage(map[domain.Coordinate]string{
		{Type: "Query", Field: "apollo"}:   "root",
		{Type: "ApolloPlan", Field: "rid"}: "reporting",
	})
	defects := domain.Defects(domain.Reconcile(declared, partial))
	if len(defects) != 1 || defects[0].Kind != domain.UnservedNative {
		t.Fatalf("an invented coordinate nothing serves was not a defect: %v", defects)
	}
	if defects[0].Coordinate.String() != "ApolloPlan.environment" {
		t.Errorf("the defect names the wrong coordinate: %s", defects[0].Coordinate)
	}
}

// A recovered coordinate Conduit cannot answer is expected -- Conduit holds no
// bundles -- so it must be reported without failing the build. If this became a
// defect, the only way to pass would be to drop Apollo's shape or to invent
// data, which are the two outcomes the interface exists to avoid.
func TestAnUnansweredRecoveredCoordinateIsReportedButNotADefect(t *testing.T) {
	declared, err := domain.ReadProvenance(wellFormed)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	coverage := domain.NewCoverage(map[domain.Coordinate]string{
		{Type: "ApolloPlan", Field: "rid"}:         "reporting",
		{Type: "ApolloPlan", Field: "environment"}: "reporting",
	})
	issues := domain.Reconcile(declared, coverage)
	if len(issues) != 1 || issues[0].Kind != domain.UnservedObserved {
		t.Fatalf("the unanswered recovered coordinate was not reported as such: %v", issues)
	}
	if defects := domain.Defects(issues); len(defects) != 0 {
		t.Fatalf("an unanswered recovered coordinate failed the build: %v", defects)
	}
}

func TestAResolverClaimingAnUndeclaredCoordinateIsADefect(t *testing.T) {
	declared, err := domain.ReadProvenance(wellFormed)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	coverage := domain.NewCoverage(map[domain.Coordinate]string{
		{Type: "Query", Field: "apollo"}:            "root",
		{Type: "ApolloPlan", Field: "rid"}:          "reporting",
		{Type: "ApolloPlan", Field: "environment"}:  "reporting",
		{Type: "ApolloPlan", Field: "secretStatus"}: "somewhere",
	})
	defects := domain.Defects(domain.Reconcile(declared, coverage))
	if len(defects) != 1 || defects[0].Kind != domain.Undeclared {
		t.Fatalf("a coordinate outside the served contract was not a defect: %v", defects)
	}
}

// Coverage must not be widenable after a resolver has been built from it.
func TestCoverageDoesNotAliasItsInput(t *testing.T) {
	sources := map[domain.Coordinate]string{{Type: "ApolloPlan", Field: "rid"}: "reporting"}
	coverage := domain.NewCoverage(sources)
	sources[domain.Coordinate{Type: "ApolloPlan", Field: "smuggled"}] = "nowhere"
	if _, found := coverage.SourceOf(domain.Coordinate{Type: "ApolloPlan", Field: "smuggled"}); found {
		t.Fatal("coverage widened after construction")
	}
}

// --- refusal ----------------------------------------------------------------

func TestARecoveredRefusalSendsTheReaderToAHubAndSaysWhyItIsNotNull(t *testing.T) {
	message := domain.RefuseUnserved(domain.Coordinate{Type: "ApolloBundle", Field: "rid"}, domain.Observed)
	for _, want := range []string{"ApolloBundle.rid", "Apollo Hub", "null"} {
		if !strings.Contains(message, want) {
			t.Errorf("the refusal does not mention %q: %s", want, message)
		}
	}
}

// An invented coordinate nothing backs is Conduit's fault, and the refusal must
// say so rather than implying the client asked for something unreasonable.
func TestAnInventedRefusalBlamesConduit(t *testing.T) {
	message := domain.RefuseUnserved(domain.Coordinate{Type: "ApolloPlan", Field: "rid"}, domain.ConduitNative)
	if !strings.Contains(message, "defect in Conduit") {
		t.Errorf("the refusal does not own the defect: %s", message)
	}
}
