// Package application projects check-ins into online, degraded, or disconnected
// and queues a change while a site cannot sync.
package application

import (
	"context"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/checkin/ports"
)

// Status is what the fleet screen reads.
type Status struct {
	Site        string      `json:"site"`
	Environment string      `json:"environment"`
	State       ports.State `json:"state"`
	LastCheckIn time.Time   `json:"lastCheckIn"`
	Pending     []string    `json:"pending"`
	SilenceKept bool        `json:"silenceKept"`
}

// Service applies a checker.
type Service struct {
	Checker ports.Checker
	// SilenceExpected environments are not marked disconnected only because a
	// report is old. The caller passes the existing connection-setting fact.
	SilenceExpected map[string]bool

	mu      sync.Mutex
	pending map[string][]string
	seen    map[string]ports.CheckIn
}

// Observe records a check-in. An expected silence does not become disconnected.
func (service *Service) Observe(ctx context.Context, site, environment string, reportOld bool) (Status, error) {
	checkin, err := service.Checker.CheckIn(ctx, site)
	if err != nil {
		return Status{}, err
	}
	if checkin.Environment == "" {
		checkin.Environment = environment
	}
	state := checkin.State
	if reportOld && service.SilenceExpected[environment] && state == ports.Disconnected {
		state = ports.Degraded
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.seen == nil {
		service.seen = map[string]ports.CheckIn{}
	}
	if service.pending == nil {
		service.pending = map[string][]string{}
	}
	checkin.State = state
	service.seen[site] = checkin
	if state != ports.Disconnected {
		delete(service.pending, site)
	}
	return service.statusLocked(site), nil
}

// Submit queues a change while the site is disconnected and does not apply it.
func (service *Service) Submit(site, change string) (Status, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.pending == nil {
		service.pending = map[string][]string{}
	}
	seen, ok := service.seen[site]
	if !ok {
		return Status{}, errNoCheckIn
	}
	if seen.State == ports.Disconnected {
		service.pending[site] = append(service.pending[site], change)
		return service.statusLocked(site), nil
	}
	return service.statusLocked(site), nil
}

func (service *Service) statusLocked(site string) Status {
	seen := service.seen[site]
	pending := append([]string(nil), service.pending[site]...)
	if pending == nil {
		pending = []string{}
	}
	return Status{
		Site: seen.Site, Environment: seen.Environment, State: seen.State,
		LastCheckIn: seen.At, Pending: pending,
	}
}

type noCheckIn struct{}

func (noCheckIn) Error() string { return "no check-in" }

var errNoCheckIn = noCheckIn{}
