// Package entityadapter turns the entities a plan installs into rollout steps.
//
// The only interesting question is how long a step takes, and the platform
// already answers it: the executor waits for resources to become ready for
// exactly the timeout configured for an install or an upgrade, and abandons the
// step when that elapses. That bound is therefore the longest a step can
// occupy its lane, so scheduling against it is scheduling against the
// executor's own contract rather than against a guess.
//
// The selection rule is copied deliberately from internal/agent/helm.go: the
// install timeout for a first revision, the upgrade timeout thereafter, and a
// fallback when the chosen one is unset. A schedule built on a different rule
// than the executor uses would be wrong in exactly the cases that matter — the
// slow ones.
package entityadapter

import (
	"fmt"
	"sort"

	"github.com/jonathanagustin/conduit/internal/delivery/rollout/domain"
)

// Installation is one entity going onto one lane, with the readiness bounds
// that apply to it.
type Installation struct {
	StepID string
	Entity string
	Lane   string
	// Upgrade is true when the release already has a revision on this lane, so
	// the upgrade timeout applies rather than the install one.
	Upgrade                 bool
	InstallReadinessSeconds int
	UpgradeReadinessSeconds int
	// Demand is what the step consumes while it runs, in the platform's own
	// capacity dimensions. Absent is not zero: a step whose demand nobody has
	// measured cannot be placed honestly, so it is refused.
	Demand map[string]int64
}

// Steps renders installations as rollout steps.
//
// The fallback is the executor's own default readiness timeout, used when an
// installation configures neither bound. Zero is refused rather than defaulted
// here, because a zero-second step would schedule as instantaneous and the
// executor would still wait.
func Steps(installations []Installation, fallbackSeconds int64) ([]domain.Step, error) {
	if fallbackSeconds <= 0 {
		return nil, fmt.Errorf("fallback readiness timeout must be positive, got %d", fallbackSeconds)
	}

	steps := make([]domain.Step, 0, len(installations))
	seen := map[string]struct{}{}

	for _, installation := range installations {
		if installation.StepID == "" || installation.Lane == "" {
			return nil, fmt.Errorf("installation is incomplete: %+v", installation)
		}
		if _, duplicate := seen[installation.StepID]; duplicate {
			return nil, fmt.Errorf("step %q appears twice", installation.StepID)
		}
		seen[installation.StepID] = struct{}{}

		if len(installation.Demand) == 0 {
			return nil, fmt.Errorf(
				"step %q declares no demand; an unmeasured step cannot be placed against a lane's capacity",
				installation.StepID,
			)
		}

		steps = append(steps, domain.Step{
			ID:              installation.StepID,
			DurationSeconds: readinessSeconds(installation, fallbackSeconds),
			EligibleLanes:   []string{installation.Lane},
			Demand:          installation.Demand,
		})
	}

	// Stable output, for the same reason the ordering is stable: one fleet must
	// compile to one problem however it was enumerated.
	sort.Slice(steps, func(i, j int) bool { return steps[i].ID < steps[j].ID })
	return steps, nil
}

// readinessSeconds mirrors readinessTimeout in internal/agent/helm.go. If that
// rule changes, this must change with it, or the schedule and the executor stop
// agreeing about how long the same work takes.
func readinessSeconds(installation Installation, fallbackSeconds int64) int64 {
	seconds := installation.InstallReadinessSeconds
	if installation.Upgrade {
		seconds = installation.UpgradeReadinessSeconds
	}
	if seconds > 0 {
		return int64(seconds)
	}
	return fallbackSeconds
}
