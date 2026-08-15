package application_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/gitless/application"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

type recorder struct {
	mutex     sync.Mutex
	deployed  []string
	unchanged []string
	refused   []string
	failed    []string
}

func (log *recorder) Deployed(environment string, _ application.Outcome) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.deployed = append(log.deployed, environment)
}

func (log *recorder) Unchanged(environment string, _ application.Outcome) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.unchanged = append(log.unchanged, environment)
}

func (log *recorder) Refused(environment string, _ []domain.Refusal) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.refused = append(log.refused, environment)
}

func (log *recorder) Failed(environment string, _ error) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.failed = append(log.failed, environment)
}

func binding(environment, channelName string) application.Binding {
	return application.Binding{Environment: environment, Selector: channel(channelName)}
}

func newScheduler(t *testing.T, parts *fixture, bindings ...application.Binding) (*application.Scheduler, *recorder) {
	t.Helper()
	log := &recorder{}
	scheduler, err := application.NewScheduler(parts.source, bindings, time.Minute, log)
	if err != nil {
		t.Fatalf("build scheduler: %v", err)
	}
	return scheduler, log
}

// A first deployment moves nothing and is emphatically not a no-op. Before
// Outcome carried Deployed, a caller could not tell "already there" from "just
// deployed for the first time" -- the difference between silence and a release
// notification.
func TestAFirstDeploymentIsReportedAsADeploymentNotAsUnchanged(t *testing.T) {
	parts := newFixture(t)
	scheduler, log := newScheduler(t, parts, binding("prod", "latest-stable"))
	scheduler.Pass(t.Context())
	if len(log.deployed) != 1 || log.deployed[0] != "prod" {
		t.Fatalf("a first deployment was not reported: deployed=%v unchanged=%v", log.deployed, log.unchanged)
	}
}

func TestASecondPassOverAnUnchangedChannelReportsUnchanged(t *testing.T) {
	parts := newFixture(t)
	scheduler, log := newScheduler(t, parts, binding("prod", "latest-stable"))
	scheduler.Pass(t.Context())
	scheduler.Pass(t.Context())
	if len(log.deployed) != 1 || len(log.unchanged) != 1 {
		t.Fatalf("deployed=%v unchanged=%v", log.deployed, log.unchanged)
	}
}

// A refusal is the system working; a registry outage is not. Reporting them the
// same way would bury the first in the noise of the second.
func TestARefusalIsReportedSeparatelyFromAFailure(t *testing.T) {
	parts := newFixture(t)
	parts.verifier.attestation.Identity = "https://github.com/attacker/conduit/.github/workflows/x.yml@refs/tags/v1.0.0"
	scheduler, log := newScheduler(t, parts, binding("prod", "latest-stable"))
	scheduler.Pass(t.Context())
	if len(log.refused) != 1 || len(log.failed) != 0 {
		t.Fatalf("refused=%v failed=%v", log.refused, log.failed)
	}

	other := newFixture(t)
	other.resolver.err = errors.New("registry unreachable")
	scheduler, log = newScheduler(t, other, binding("prod", "latest-stable"))
	scheduler.Pass(t.Context())
	if len(log.failed) != 1 || len(log.refused) != 0 {
		t.Fatalf("refused=%v failed=%v", log.refused, log.failed)
	}
}

// One environment's registry being unreachable must not stop the others being
// reconciled. A scheduler that gave up on the first error would let one broken
// source freeze the fleet.
func TestOneFailingEnvironmentDoesNotStopTheOthers(t *testing.T) {
	parts := newFixture(t)
	parts.converger.deployedErr = nil
	scheduler, log := newScheduler(t, parts,
		binding("alpha", "latest-stable"), binding("beta", "latest-stable"), binding("gamma", "latest-stable"))
	scheduler.Pass(t.Context())
	if len(log.deployed)+len(log.unchanged) != 3 {
		t.Fatalf("not every environment was reconciled: deployed=%v unchanged=%v", log.deployed, log.unchanged)
	}
}

// A Hub that restarted should not wait an interval before noticing its
// environments are behind.
func TestRunReconcilesImmediatelyRatherThanAfterTheFirstInterval(t *testing.T) {
	parts := newFixture(t)
	log := &recorder{}
	scheduler, err := application.NewScheduler(parts.source, []application.Binding{binding("prod", "latest-stable")}, time.Hour, log)
	if err != nil {
		t.Fatalf("build scheduler: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { scheduler.Run(ctx); close(done) }()

	deadline := time.After(5 * time.Second)
	for {
		log.mutex.Lock()
		deployed := len(log.deployed)
		log.mutex.Unlock()
		if deployed > 0 {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("Run did not reconcile before the first interval elapsed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop when its context was cancelled")
	}
}

// --- construction -----------------------------------------------------------

// Two selectors for one environment is not a merge, it is a race: each pass
// would deploy whichever ran last.
func TestAnEnvironmentBoundTwiceIsRefused(t *testing.T) {
	parts := newFixture(t)
	_, err := application.NewScheduler(parts.source,
		[]application.Binding{binding("prod", "latest-stable"), binding("prod", "latest")}, time.Minute, &recorder{})
	if err == nil {
		t.Fatal("an environment bound to two sources was accepted")
	}
	if !strings.Contains(err.Error(), "more than one source") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// A malformed selector found on the first tick would be a Hub that started
// cleanly and then silently never deployed one of its environments.
func TestAMalformedBindingFailsAtStartupRatherThanOnTheFirstTick(t *testing.T) {
	parts := newFixture(t)
	broken := application.Binding{Environment: "prod", Selector: domain.Selector{Kind: domain.ByChannel}}
	if _, err := application.NewScheduler(parts.source, []application.Binding{broken}, time.Minute, &recorder{}); err == nil {
		t.Fatal("a malformed binding was accepted")
	}
}

func TestASchedulerMustBeUsable(t *testing.T) {
	parts := newFixture(t)
	good := []application.Binding{binding("prod", "latest-stable")}
	for name, build := range map[string]func() (*application.Scheduler, error){
		"no bindings": func() (*application.Scheduler, error) {
			return application.NewScheduler(parts.source, nil, time.Minute, &recorder{})
		},
		"no observer": func() (*application.Scheduler, error) {
			return application.NewScheduler(parts.source, good, time.Minute, nil)
		},
		"no source": func() (*application.Scheduler, error) {
			return application.NewScheduler(nil, good, time.Minute, &recorder{})
		},
		"zero interval": func() (*application.Scheduler, error) {
			return application.NewScheduler(parts.source, good, 0, &recorder{})
		},
	} {
		if _, err := build(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestBindingsAreListedInAStableOrder(t *testing.T) {
	parts := newFixture(t)
	scheduler, _ := newScheduler(t, parts,
		binding("gamma", "latest"), binding("alpha", "latest"), binding("beta", "latest"))
	names := make([]string, 0, 3)
	for _, bound := range scheduler.Bindings() {
		names = append(names, bound.Environment)
	}
	if strings.Join(names, ",") != "alpha,beta,gamma" {
		t.Fatalf("bindings are not stably ordered: %v", names)
	}
}
