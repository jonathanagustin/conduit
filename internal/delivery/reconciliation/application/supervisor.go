package application

import (
	"context"
	"errors"
	"time"
)

type Synchronizer interface {
	Sync(context.Context) error
}

type SupervisorConfig struct {
	PollInterval time.Duration
	SyncInterval time.Duration
	OnError      func(error)
}

// Supervisor owns the bounded process loop around one durable Worker. Every
// Hub replica may run it: database leases select winners, while periodic Sync
// repairs the legacy-to-context dual-write window from committed evidence.
type Supervisor struct {
	worker       *Worker
	synchronizer Synchronizer
	config       SupervisorConfig
}

func NewSupervisor(worker *Worker, synchronizer Synchronizer, config SupervisorConfig) (*Supervisor, error) {
	if worker == nil || synchronizer == nil {
		return nil, errors.New("reconciliation worker and synchronizer are required")
	}
	if config.PollInterval <= 0 || config.SyncInterval <= 0 {
		return nil, errors.New("reconciliation poll and sync intervals must be positive")
	}
	return &Supervisor{worker: worker, synchronizer: synchronizer, config: config}, nil
}

// Prime is a startup gate. The Hub does not advertise readiness until committed
// plans/reports are synchronized, dropped work is repaired, and the available
// queue has been drained once.
func (supervisor *Supervisor) Prime(ctx context.Context) error {
	if err := supervisor.synchronizer.Sync(ctx); err != nil {
		return err
	}
	if _, err := supervisor.worker.Repair(ctx); err != nil {
		return err
	}
	return supervisor.worker.Drain(ctx)
}

func (supervisor *Supervisor) Run(ctx context.Context) {
	poll := time.NewTicker(supervisor.config.PollInterval)
	sync := time.NewTicker(supervisor.config.SyncInterval)
	defer poll.Stop()
	defer sync.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			supervisor.report(supervisor.worker.Drain(ctx))
		case <-sync.C:
			if err := supervisor.synchronizer.Sync(ctx); err != nil {
				supervisor.report(err)
				continue
			}
			if _, err := supervisor.worker.Repair(ctx); err != nil {
				supervisor.report(err)
				continue
			}
			supervisor.report(supervisor.worker.Drain(ctx))
		}
	}
}

func (supervisor *Supervisor) report(err error) {
	if err != nil && supervisor.config.OnError != nil {
		supervisor.config.OnError(err)
	}
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }
