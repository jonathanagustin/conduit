package application

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollographql/domain"
)

// countedClaim is one number a document publishes about the interface, together
// with the sentence it lives in.
//
// The pattern carries the sentence deliberately rather than matching a bare
// digit. A regexp of `(\d+)` would keep passing while the claim around it was
// rewritten into something the number no longer answers, which is the failure
// this file exists to catch: every count corrected here was individually
// plausible and collectively impossible.
type countedClaim struct {
	path    string
	pattern string
	want    []int
	says    string
}

// derived is what the code actually says, computed once and compared against
// every document. Nothing here restates a number: the counts come from the SDL
// and the coverage table, so correcting a document can never be done by editing
// this test to agree with it.
type derived struct {
	observed, documented, native int
	declared, served, refused    int
	observedServed               int
}

func deriveCounts(t *testing.T) derived {
	t.Helper()
	sdl, err := os.ReadFile("../schema/apollo.interface.graphql")
	if err != nil {
		t.Fatal(err)
	}
	declared, err := domain.ReadProvenance(string(sdl))
	if err != nil {
		t.Fatal(err)
	}
	counts := derived{}
	counts.observed, counts.documented, counts.native = declared.Count()
	counts.declared = counts.observed + counts.documented + counts.native
	// Served is counted by asking coverage about every declared coordinate
	// rather than by taking len() of the table. The two agree today, and if they
	// ever stop -- a coverage entry for a coordinate the SDL no longer declares
	// -- the number a document should publish is the one a client can reach.
	for _, coordinate := range declared.Coordinates() {
		if _, found := Coverage().SourceOf(coordinate); found {
			counts.served++
		}
	}
	for _, issue := range domain.Reconcile(declared, Coverage()) {
		if issue.Kind == domain.UnservedObserved {
			counts.refused++
		}
	}
	counts.observedServed = counts.observed - counts.refused
	return counts
}

// TestPublishedCoordinateCountsMatchTheInterface pins every document that
// states how much of the interface is served.
//
// Until this existed, one document was pinned -- the ADR, by
// TestRefusedCoordinatesMatchTheDecisionRecord -- and four were not. All four
// drifted, and they drifted in the way an unpinned number does: three still
// said 18 refused coordinates long after the count settled at 12, and the
// published table said "Recovered coordinates served | 41" directly above
// "Recovered coordinates declared but unanswerable | 12", which is 53 of a
// possible 41. Nobody notices arithmetic in prose. A test does.
//
// The reasoning that pinned the ADR applies verbatim to the documents that
// repeat its figures; that it was applied to one file and not the others is the
// whole defect.
func TestPublishedCoordinateCountsMatchTheInterface(t *testing.T) {
	t.Parallel()
	counts := deriveCounts(t)

	// Guard the derivation itself. If ReadProvenance or Reconcile silently
	// returned nothing, every claim below would be compared against zero and a
	// document could publish anything. This is the one place a literal is
	// allowed, and it is a floor rather than an equality so that widening the
	// interface does not fail here instead of where it should.
	if counts.declared < 60 || counts.served < 45 || counts.refused == 0 {
		t.Fatalf("the derivation looks broken: declared %d, served %d, refused %d",
			counts.declared, counts.served, counts.refused)
	}
	if counts.observedServed+counts.documented+counts.native != counts.served {
		t.Fatalf("served %d does not decompose: %d observed-served + %d documented + %d native",
			counts.served, counts.observedServed, counts.documented, counts.native)
	}

	claims := []countedClaim{{
		path:    "../../../docs/decisions/0007-registered-invention-over-refusal.md",
		pattern: `(\d+) recovered coordinates declared, of which (\d+) are served and (\d+) (?:are|is) unanswerable`,
		want:    []int{counts.observed, counts.observedServed, counts.refused},
		says:    "the ADR's consequences",
	}, {
		path:    "../../../docs/apollo/reference/graphql-api.md",
		pattern: `\| Recovered coordinates declared \| (\d+) \| \| — of which served \| (\d+) \| \| — of which declared but unanswerable \| (\d+) \|`,
		want:    []int{counts.observed, counts.observedServed, counts.refused},
		says:    "the published property table",
	}, {
		path:    "../../../docs/apollo/reference/graphql-api.md",
		pattern: `\| Coordinates served, total \| (\d+) \((\d+) .OBSERVED. \+ (\d+) .DOCUMENTED.\) \|`,
		want:    []int{counts.served, counts.observedServed, counts.documented},
		says:    "the served total and its decomposition",
	}, {
		path:    "../../../docs/apollo/reference/graphql-api.md",
		pattern: `(\d+) \(35% of the (\d+) declared; 35% of the (\d+) served\)`,
		want:    []int{counts.documented, counts.declared, counts.served},
		says:    "the uncaptured share and both of its denominators",
	}, {
		path:    "../../../docs/apollo/conformance.md",
		pattern: `(\d+) recovered coordinates? (?:are|is) declared and refused`,
		want:    []int{counts.refused},
		says:    "the conformance summary's refusal count",
	}, {
		path:    "../../../docs/apollo/conformance.md",
		pattern: `declares\*\* (\d+) .OBSERVED. coordinates plus (\d+) .DOCUMENTED. ones`,
		want:    []int{counts.observed, counts.documented},
		says:    "the conformance row's tier counts",
	}, {
		path:    "../../../docs/apollo/conformance.md",
		pattern: `(\d+) of the (\d+) are served, the other (\d+) refuse`,
		want:    []int{counts.served, counts.declared, counts.refused},
		says:    "the conformance row's served split",
	}}

	documents := map[string]string{}
	for _, claim := range claims {
		if _, loaded := documents[claim.path]; loaded {
			continue
		}
		contents, err := os.ReadFile(claim.path)
		if err != nil {
			t.Fatal(err)
		}
		// Markdown wraps, and a claim that spans a line break is still one
		// claim. Collapsing runs of whitespace lets the patterns read like the
		// sentences do rather than encoding where the author happened to wrap.
		documents[claim.path] = strings.Join(strings.Fields(string(contents)), " ")
	}

	for _, claim := range claims {
		matches := regexp.MustCompile(claim.pattern).FindAllStringSubmatch(documents[claim.path], -1)
		if len(matches) == 0 {
			t.Errorf("%s no longer states %s; the sentence /%s/ matching it was removed or reworded, "+
				"so the count it carried is now unpinned -- update this test with the new wording, "+
				"do not delete the claim", claim.path, claim.says, claim.pattern)
			continue
		}
		if len(matches) > 1 {
			t.Errorf("%s states %s %d times; a repeated count is a count that can disagree with itself",
				claim.path, claim.says, len(matches))
			continue
		}
		got := matches[0][1:]
		if len(got) != len(claim.want) {
			t.Fatalf("%s: the pattern for %s captures %d numbers but %d are expected",
				claim.path, claim.says, len(got), len(claim.want))
		}
		for index, text := range got {
			value, err := strconv.Atoi(text)
			if err != nil {
				t.Fatalf("%s: %s captured %q, which is not a number: %v", claim.path, claim.says, text, err)
			}
			if value != claim.want[index] {
				t.Errorf("%s: %s says %d where the interface has %d (%s)",
					claim.path, claim.says, value, claim.want[index], explainCounts(counts))
			}
		}
	}
}

func explainCounts(counts derived) string {
	return fmt.Sprintf("declared %d = %d OBSERVED + %d DOCUMENTED + %d CONDUIT_NATIVE; "+
		"served %d = %d observed-served + %d documented; refused %d",
		counts.declared, counts.observed, counts.documented, counts.native,
		counts.served, counts.observedServed, counts.documented, counts.refused)
}
