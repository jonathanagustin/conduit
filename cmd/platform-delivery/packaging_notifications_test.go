package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	packagingconfig "github.com/jonathanagustin/conduit/internal/packagingworkflow/config"
)

func TestPackagingWorkerRefusesNotificationStartupFailureBeforeAnyProposal(t *testing.T) {
	called := false
	err := runPackagingWorker(t.Context(), func(name string) string {
		if name == "REPOSITORY_KIND" {
			return "postgres"
		}
		return ""
	}, packagingconfig.Config{}, controlplane.NewMemoryRepository(), func(context.Context, packagingconfig.Config, controlplane.Repository) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Fatalf("packaging source ran before durable notification setup succeeded: called=%v error=%v", called, err)
	}
}

func TestPackagingWorkerFileModeKeepsItsOptionalNoStoreBehavior(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	want := errors.New("worker stopped")
	called := false
	var child context.Context
	err := runPackagingWorker(t.Context(), func(string) string { return "" }, packagingconfig.Config{}, repository, func(ctx context.Context, _ packagingconfig.Config, received controlplane.Repository) error {
		called, child = true, ctx
		if received != repository {
			t.Fatal("worker did not receive its configured repository")
		}
		if _, configured := received.TransactionAnnouncements(); configured {
			t.Fatal("file mode invented durable notification intent")
		}
		return want
	})
	if !called || !errors.Is(err, want) {
		t.Fatalf("worker outcome changed: called=%v error=%v", called, err)
	}
	if child.Err() == nil {
		t.Fatal("worker exit did not cancel its owned context")
	}
}

func TestPackagingWorkerStopsAdmissionBeforeClosingStoresOnEveryWorkerExit(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "cancellation"} {
		t.Run(outcome, func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			started, stopped := make(chan struct{}), make(chan struct{})
			setupComplete, closed := false, false
			failure := errors.New("Temporal worker refused startup")
			configure := func(context.Context, func(string) string, controlplane.Repository) (packagingNotificationSources, error) {
				setupComplete = true
				return packagingNotificationSources{
					run: func(ctx context.Context) {
						close(started)
						<-ctx.Done()
						close(stopped)
					},
					close: func() {
						select {
						case <-stopped:
						default:
							t.Error("notification pool closed while admission was still running")
						}
						closed = true
					},
				}, nil
			}
			worker := func(ctx context.Context, _ packagingconfig.Config, _ controlplane.Repository) error {
				if !setupComplete {
					t.Error("worker entered before notification factory configuration")
				}
				select {
				case <-started:
				case <-time.After(time.Second):
					return errors.New("notification admission was not started")
				}
				switch outcome {
				case "failure":
					return failure
				case "cancellation":
					cancel()
					<-ctx.Done()
					return ctx.Err()
				default:
					return nil
				}
			}
			err := runPackagingWorkerWithNotifications(parent, func(string) string { return "" }, packagingconfig.Config{}, controlplane.NewMemoryRepository(), worker, configure)
			if !closed || (outcome == "success" && err != nil) || (outcome == "failure" && !errors.Is(err, failure)) || (outcome == "cancellation" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("worker lifecycle lost its outcome or notification cleanup: closed=%v error=%v", closed, err)
			}
		})
	}
}
