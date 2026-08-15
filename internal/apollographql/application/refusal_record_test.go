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
func recordedRefusals(t *testing.T, record string) (map[string]struct{}, int) {
	t.Helper()
	recorded, declaredTotal := map[string]struct{}{}, 0
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
		declaredTotal += declared
		found := 0
		for cursor := index + 1; cursor < len(lines); cursor++ {
			if strings.TrimSpace(lines[cursor]) == "" {
				if found > 0 {
					break // end of the enumeration paragraph
				}
				continue // blank line between the heading and it
			}
			for _, match := range backtickedCoordinate.FindAllStringSubmatch(lines[cursor], -1) {
				recorded[match[1]+"."+match[2]] = struct{}{}
				found++
			}
			if found > 0 && !strings.HasSuffix(strings.TrimSpace(lines[cursor]), ",") &&
				!strings.HasSuffix(strings.TrimSpace(lines[cursor]), "`") {
				break
			}
		}
		if found != declared {
			t.Errorf("%q claims %d coordinates but lists %d", strings.TrimSpace(line), declared, found)
		}
	}
	return recorded, declaredTotal
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
	recorded, declaredTotal := recordedRefusals(t, string(record))
	if declaredTotal != len(refused) {
		t.Errorf("the record's group headings sum to %d, but %d coordinates refuse", declaredTotal, len(refused))
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
			len(refused), len(recorded), sortedKeys(refused), sortedKeys(recorded))
	}
}

func sortedKeys(set map[string]struct{}) string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
