package entityadapter

import (
	"strings"
	"testing"
)

func install() Installation {
	return Installation{
		StepID: "a-agent", Entity: "agent", Lane: "spoke-a",
		InstallReadinessSeconds: 300, UpgradeReadinessSeconds: 600,
		Demand: map[string]int64{"podSlots": 1},
	}
}

// The rule is the executor's, not this package's: install timeout on a first
// revision, upgrade timeout thereafter. A schedule built on a different rule
// would be wrong in exactly the slow cases that matter.
func TestDurationFollowsTheExecutorsReadinessRule(t *testing.T) {
	fresh := install()
	upgrade := install()
	upgrade.Upgrade = true

	steps, err := Steps([]Installation{fresh}, 900)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].DurationSeconds != 300 {
		t.Fatalf("a first revision should take the install timeout, got %d", steps[0].DurationSeconds)
	}

	steps, err = Steps([]Installation{upgrade}, 900)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].DurationSeconds != 600 {
		t.Fatalf("an upgrade should take the upgrade timeout, got %d", steps[0].DurationSeconds)
	}
}

func TestAnUnsetTimeoutFallsBackRatherThanScheduldingAsInstant(t *testing.T) {
	unset := install()
	unset.InstallReadinessSeconds = 0

	steps, err := Steps([]Installation{unset}, 900)
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].DurationSeconds != 900 {
		t.Fatalf("an unset timeout should fall back, got %d", steps[0].DurationSeconds)
	}
}

// A zero-second step would schedule as instantaneous while the executor still
// waits, so the fallback itself must be real.
func TestAZeroFallbackIsRefused(t *testing.T) {
	if _, err := Steps([]Installation{install()}, 0); err == nil {
		t.Fatal("a zero fallback was accepted")
	}
}

// Absent demand is not zero demand. A step nobody has measured cannot be placed
// against a lane's capacity honestly.
func TestAStepWithNoDemandIsRefused(t *testing.T) {
	unmeasured := install()
	unmeasured.Demand = nil

	_, err := Steps([]Installation{unmeasured}, 900)
	if err == nil {
		t.Fatal("an unmeasured step was accepted")
	}
	if !strings.Contains(err.Error(), "no demand") {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

func TestDuplicateStepsAreRefused(t *testing.T) {
	if _, err := Steps([]Installation{install(), install()}, 900); err == nil {
		t.Fatal("a duplicate step id was accepted")
	}
}

func TestIncompleteInstallationsAreRefused(t *testing.T) {
	for name, mutate := range map[string]func(*Installation){
		"no step id": func(i *Installation) { i.StepID = "" },
		"no lane":    func(i *Installation) { i.Lane = "" },
	} {
		t.Run(name, func(t *testing.T) {
			incomplete := install()
			mutate(&incomplete)
			if _, err := Steps([]Installation{incomplete}, 900); err == nil {
				t.Fatal("an incomplete installation was accepted")
			}
		})
	}
}

func TestOutputIsCanonicalWhateverOrderTheInputArrives(t *testing.T) {
	first := install()
	first.StepID = "a-first"
	second := install()
	second.StepID = "b-second"

	forward, err := Steps([]Installation{first, second}, 900)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := Steps([]Installation{second, first}, 900)
	if err != nil {
		t.Fatal(err)
	}

	for index := range forward {
		if forward[index].ID != reverse[index].ID {
			t.Fatalf("order depended on input:\n  %+v\n  %+v", forward, reverse)
		}
	}
}
