package domain

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// constantNames is how an emit site spells each type. It is restated here
// because a test cannot enumerate another package's identifiers, and the
// length check below is what stops a type added to the vocabulary from being
// left out of it.
var constantNames = map[EventType]string{
	EventLatestReleaseVulnerability:    "EventLatestReleaseVulnerability",
	EventInstalledReleaseVulnerability: "EventInstalledReleaseVulnerability",
	EventPromotionFailed:               "EventPromotionFailed",
	EventProductOpenEndedRecall:        "EventProductOpenEndedRecall",
	EventBlueGreenUpgradeFailed:        "EventBlueGreenUpgradeFailed",
	EventRoleChange:                    "EventRoleChange",
	EventProductRecalls:                "EventProductRecalls",
	EventRBACChange:                    "EventRBACChange",
	EventChangeRequestPendingApproval:  "EventChangeRequestPendingApproval",
	EventPlanFailures:                  "EventPlanFailures",
}

// Raised is a claim about other packages, and a claim nothing checks is how
// seven of ten types came to be offered and never raised. This reads the
// non-test source of every package outside this one and holds the set to it in
// both directions: a type marked raised that no adapter names is the old
// defect, and a type an adapter names that is not marked raised would be
// reported to a subscriber as one that will not fire.
//
// It proves a reference, not a reachable call. That an adapter is wired and
// invoked is what the composition tests are for.
func TestRaisedTypesHaveAnEmitSite(t *testing.T) {
	t.Parallel()
	if len(constantNames) != len(EventTypes()) {
		t.Fatalf("%d types are known and %d are named here", len(EventTypes()), len(constantNames))
	}
	internal, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(internal, "notifications", "domain")
	referenced := map[EventType]string{}
	err = filepath.WalkDir(internal, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == own {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for eventType, name := range constantNames {
			if strings.Contains(string(source), "."+name) {
				referenced[eventType] = path
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, eventType := range EventTypes() {
		path, found := referenced[eventType]
		switch {
		case eventType.Raised() && !found:
			t.Errorf("%s is reported as raised and no package outside the domain names it", eventType)
		case !eventType.Raised() && found:
			t.Errorf("%s is named by %s and is reported to subscribers as not yet raised", eventType, path)
		}
	}
}

func TestNotYetRaisedNamesOnlyKnownTypesNothingRaises(t *testing.T) {
	t.Parallel()
	got := NotYetRaised([]EventType{
		EventPlanFailures, EventBlueGreenUpgradeFailed, "A_TYPE_FROM_A_LATER_BUILD",
		EventChangeRequestPendingApproval, EventRBACChange, EventLatestReleaseVulnerability,
		EventInstalledReleaseVulnerability, EventRoleChange, EventPromotionFailed,
		EventProductRecalls, EventProductOpenEndedRecall,
	})
	want := []EventType{
		EventBlueGreenUpgradeFailed, EventRBACChange,
		EventLatestReleaseVulnerability, EventInstalledReleaseVulnerability,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("not yet raised = %v, want %v", got, want)
	}
	if NotYetRaised([]EventType{EventPlanFailures}) != nil {
		t.Error("a preference whose every type is raised was reported as having some that are not")
	}
	// Unavailable for explicit selection is distinct from unknown vocabulary.
	for _, eventType := range want {
		if !eventType.Valid() {
			t.Errorf("%s is not raised and was also made unknown", eventType)
		}
	}
}

func TestExplicitPreferenceSelectionRequiresAnEmitSite(t *testing.T) {
	t.Parallel()
	for _, eventType := range EventTypes() {
		preference := Preference{Types: []EventType{EventPlanFailures, eventType}}
		err := preference.ValidateSelection()
		if eventType.Raised() && err != nil {
			t.Errorf("raised type %s was refused: %v", eventType, err)
		}
		if !eventType.Raised() && (err == nil || !strings.Contains(err.Error(), string(eventType))) {
			t.Errorf("unraised type %s was not named in its refusal: %v", eventType, err)
		}
		preference.AllFutureTypes = true
		if err := preference.ValidateSelection(); err != nil {
			t.Errorf("forward consent carrying %s was refused: %v", eventType, err)
		}
	}
}
