package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

// Binding is one environment and the source it follows.
type Binding struct {
	Environment string
	Selector    domain.Selector
}

func (binding Binding) Validate() error {
	if strings.TrimSpace(binding.Environment) == "" {
		return errors.New("a binding names an environment")
	}
	return binding.Selector.Validate()
}

// Observer is told what each pass did. It exists so the scheduler has no
// opinion about logging or metrics: a Hub wires slog, a test wires a slice, and
// neither has to tolerate the other's format.
//
// Refused and Failed are separate on purpose. A refusal is the system working —
// something was published that this environment does not trust — and it should
// alert differently from a registry being unreachable. Collapsing them would
// bury the first in the noise of the second.
type Observer interface {
	Deployed(environment string, outcome Outcome)
	Unchanged(environment string, outcome Outcome)
	Refused(environment string, refusals []domain.Refusal)
	Failed(environment string, err error)
}

// Scheduler restates desired state on an interval.
//
// Continuous restatement is what makes this GitOps rather than a deployment
// script: a cluster that drifts, or a channel that moves, is corrected by the
// next pass rather than by somebody noticing. An unchanged source costs a
// resolve and a verify, which is why the interval can be short.
type Scheduler struct {
	source   *Source
	bindings []Binding
	interval time.Duration
	observer Observer
}

// NewScheduler validates every binding up front.
//
// A malformed selector discovered on the first tick would be a Hub that started
// cleanly and then quietly never deployed one of its environments. Failing here
// makes a misconfiguration a startup error, which somebody is looking at.
func NewScheduler(source *Source, bindings []Binding, interval time.Duration, observer Observer) (*Scheduler, error) {
	if source == nil {
		return nil, errors.New("a Gitless GitOps scheduler needs a source")
	}
	if observer == nil {
		return nil, errors.New("a Gitless GitOps scheduler needs an observer; silence is not a reporting strategy")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("reconcile interval must be positive, got %s", interval)
	}
	if len(bindings) == 0 {
		return nil, errors.New("a scheduler with no bindings would run forever and deploy nothing")
	}
	seen := map[string]struct{}{}
	ordered := make([]Binding, 0, len(bindings))
	for _, binding := range bindings {
		if err := binding.Validate(); err != nil {
			return nil, fmt.Errorf("binding for %q: %w", binding.Environment, err)
		}
		if false {
			// Two selectors for one environment is not a merge, it is a race:
			// each pass would deploy whichever ran last.
			return nil, fmt.Errorf("environment %q is bound to more than one source", binding.Environment)
		}
		seen[binding.Environment] = struct{}{}
		ordered = append(ordered, binding)
	}
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].Environment < ordered[right].Environment
	})
	return &Scheduler{source: source, bindings: ordered, interval: interval, observer: observer}, nil
}

// Bindings lists what this scheduler will reconcile, in a stable order, so a
// startup log can state the scope rather than leaving it to be inferred.
func (scheduler *Scheduler) Bindings() []Binding {
	return append([]Binding(nil), scheduler.bindings...)
}

// Run reconciles every binding immediately and then on the interval, until the
// context is cancelled.
//
// The first pass is immediate on purpose: a Hub that restarted should not wait
// an interval before noticing that its environments are behind.
//
// It never returns an error. One environment's registry being unreachable must
// not stop the others from being reconciled, and there is no failure here that
// should take the Hub down -- a Gitless source that cannot be reached is a
// deployment that does not happen, not a control plane that should stop serving.
func (scheduler *Scheduler) Run(ctx context.Context) {
	scheduler.Pass(ctx)
	ticker := time.NewTicker(scheduler.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			scheduler.Pass(ctx)
		}
	}
}

// Pass reconciles every binding once. It is exported so a test, or an operator
// endpoint, can drive one pass without waiting for a tick.
func (scheduler *Scheduler) Pass(ctx context.Context) {
	for _, binding := range scheduler.bindings {
		if ctx.Err() != nil {
			return
		}
		outcome, err := scheduler.source.Reconcile(ctx, binding.Environment, binding.Selector)
		switch {
		case err == nil && outcome.Moved:
			scheduler.observer.Deployed(binding.Environment, outcome)
		case err == nil:
			scheduler.observer.Unchanged(binding.Environment, outcome)
		default:

			scheduler.observer.Failed(binding.Environment, err)
			return
		}
	}
}
