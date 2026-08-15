package application

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollographql/domain"
)

// decisionRecord is the ADR enumerating every coordinate that stays refused.
const decisionRecord = "../../../docs/decisions/0005-apollo-coordinates-that-stay-refused.md"

// backtickedCoordinate matches the `ApolloType.field` spellings the record uses
// in its group listings. The leading Apollo prefix is what keeps this from
// also matching the prose's references to Go identifiers and file paths.
var backtickedCoordinate = regexp.MustCompile("`(Apollo[A-Za-z]+)\\.([a-zA-Z_][a-zA-Z0-9_]*)`")

// groupHeading introduces one reason-for-refusal and declares how many
// coordinates share it.
var groupHeading = regexp.MustCompile(`^### Group \d+ .*\((\d+)\)\s*$`)

// recordedRefusals reads only the enumeration paragraph under each "### Group"
// heading, not the whole record.
//
// Scanning the whole file was the first attempt and it was wrong in a way worth
// keeping a note about: the record argues its case partly by naming coordinates
// that ARE served -- ApolloEnvironment.id is the precedent the bundle argument
// fails to meet -- so a whole-file scan read those counter-examples as members
// of the refused set. The prose has to be free to mention a served coordinate
// without that changing what the record claims.
// Returned per group, not merged, because the record's value is that each
// coordinate is filed under the reason that would actually close it. A single
// flat set would let ApolloBundle.rid and ApolloBundle.spaceId swap groups --
// "waiting on a capture" and "waiting on the Space decision" are different
// answers to "when does this close?" -- with every count and every set
// comparison unchanged, and the test green.
func recordedRefusals(t *testing.T, record string) map[string]map[string]struct{} {
	t.Helper()
	groups := map[string]map[string]struct{}{}
	lines := strings.Split(record, "\n")
	for index, line := range lines {
		heading := groupHeading.FindStringSubmatch(line)
		if heading == nil {
			continue
		}
		declared, err := strconv.Atoi(heading[1])
		if err != nil {
			t.Fatalf("group heading %q does not state a count: %v", line, err)
		}
		title := strings.TrimSpace(line)
		members := map[string]struct{}{}
		found := 0
		// The paragraph ends at a blank line and nowhere else. An earlier
		// version also stopped on a line that did not end in "," or "`", which
		// meant a coordinate on the line after one ending in "." was skipped
		// silently -- so a served coordinate could be left recorded as refused
		// without any count moving.
		for cursor := index + 1; cursor < len(lines); cursor++ {
			if strings.TrimSpace(lines[cursor]) == "" {
				if found > 0 {
					break
				}
				continue // blank line between the heading and the paragraph
			}
			for _, match := range backtickedCoordinate.FindAllStringSubmatch(lines[cursor], -1) {
				members[match[1]+"."+match[2]] = struct{}{}
				found++
			}
		}
		if found != declared {
			t.Errorf("%q claims %d coordinates but lists %d", title, declared, found)
		}
		if len(members) != found {
			t.Errorf("%q lists the same coordinate more than once", title)
		}
		groups[title] = members
	}
	return groups
}

// TestRefusedCoordinatesMatchTheDecisionRecord pins the ADR against the code.
//
// A decision record listing twelve coordinates is worth having only while it is
// still twelve. Without this, serving one of them leaves a document confidently
// explaining why an answered field cannot be answered, and declaring a
// thirteenth leaves it silently incomplete -- both of which read as current,
// which is the failure mode a stale record has and no record does not.
//
// It reads the record rather than restating the list, so there is one statement
// of what is refused and this asks whether the code agrees with it.
func TestRefusedCoordinatesMatchTheDecisionRecord(t *testing.T) {
	t.Parallel()
	sdl, err := os.ReadFile("../schema/apollo.interface.graphql")
	if err != nil {
		t.Fatal(err)
	}
	declared, err := domain.ReadProvenance(string(sdl))
	if err != nil {
		t.Fatal(err)
	}
	refused := map[string]struct{}{}
	for _, issue := range domain.Reconcile(declared, Coverage()) {
		if issue.Kind == domain.UnservedObserved {
			refused[issue.Coordinate.String()] = struct{}{}
		}
	}

	record, err := os.ReadFile(decisionRecord)
	if err != nil {
		t.Fatal(err)
	}
	groups := recordedRefusals(t, string(record))
	recorded := map[string]string{}
	for title, members := range groups {
		for coordinate := range members {
			if existing, duplicated := recorded[coordinate]; duplicated {
				t.Errorf("%s is filed under both %q and %q; it has one reason, not two", coordinate, existing, title)
			}
			recorded[coordinate] = title
		}
	}

	for coordinate := range refused {
		if _, found := recorded[coordinate]; !found {
			t.Errorf("%s refuses at request time but %s does not account for it", coordinate, decisionRecord)
		}
	}
	for coordinate := range recorded {
		if _, found := refused[coordinate]; !found {
			t.Errorf("%s says %s stays refused, but it is served now -- update the record", decisionRecord, coordinate)
		}
	}
	if len(refused) != len(recorded) {
		t.Fatalf("refused %d coordinates, record accounts for %d: %s vs %s",
			len(refused), len(recorded), sortedKeys(refused), sortedNames(recorded))
	}

	// The groups are the reasons, so they are pinned by name. Moving a
	// coordinate between them changes what the repository claims would close it
	// -- a capture that may never come, versus a decision somebody could make
	// this week -- and every count above survives that move unchanged.
	for coordinate, title := range map[string]string{
		"ApolloBundle.rid":              "### Group 1 — no Apollo resource identifiers (6)",
		"ApolloQuery.bundle":            "### Group 1 — no Apollo resource identifiers (6)",
		"ApolloProductRelease.rid":      "### Group 1 — no Apollo resource identifiers (6)",
		"ApolloBundle.spaceId":          "### Group 2 — no Space tenancy boundary (5)",
		"ApolloEnvironment.spaceId":     "### Group 2 — no Space tenancy boundary (5)",
		"ApolloEnvironment.namespaceId": "### Group 3 — no namespace (1)",
	} {
		if got := recorded[coordinate]; got != title {
			t.Errorf("%s is filed under %q, want %q", coordinate, got, title)
		}
	}
}

// sortedNames is the same rendering for the coordinate-to-group map, kept
// separate rather than made generic because the two are read side by side in
// one failure message and a shared signature would obscure which is which.
func sortedNames(set map[string]string) string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func sortedKeys(set map[string]struct{}) string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
