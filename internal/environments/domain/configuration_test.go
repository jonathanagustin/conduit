package domain

import (
	"testing"
	"time"
)

// The window is a floor, not an option. A policy that names no scan age used to
// mean "do not check", which let an Environment requiring PASS run indefinitely
// on evidence that predated the CVE (#54). The property under test is that the
// unstated case resolves to a real, positive window and the stated case is
// obeyed verbatim in both directions -- shorter and longer than the default.
func TestEffectiveMaximumScanAgeGivesEveryPolicyAWindow(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		seconds int
		want    time.Duration
	}{
		"unstated falls back to the default floor": {0, DefaultMaximumScanAgeSeconds * time.Second},
		"a negative bound is not an escape hatch":  {-1, DefaultMaximumScanAgeSeconds * time.Second},
		"a stated bound tighter than the default":  {3600, time.Hour},
		"a stated bound wider than the default":    {30 * 24 * 3600, 30 * 24 * time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			policy := ArtifactSecurityPolicy{RequiredOutcome: "PASS", MaximumScanAgeSeconds: testCase.seconds}
			if got := policy.EffectiveMaximumScanAge(); got != testCase.want {
				t.Fatalf("EffectiveMaximumScanAge() = %s, want %s", got, testCase.want)
			}
		})
	}
	// Stated separately from the table because it is the property, not a case:
	// no policy resolves to an unbounded or zero window, whatever it says.
	for _, seconds := range []int{-86400, -1, 0, 1} {
		policy := ArtifactSecurityPolicy{MaximumScanAgeSeconds: seconds}
		if window := policy.EffectiveMaximumScanAge(); window <= 0 || window > DefaultMaximumScanAgeSeconds*time.Second {
			t.Fatalf("maximumScanAgeSeconds %d resolved to %s, which is not a bounded positive window", seconds, window)
		}
	}
}
