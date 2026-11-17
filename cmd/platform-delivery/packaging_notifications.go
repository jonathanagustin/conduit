package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	notificationpostgres "github.com/jonathanagustin/conduit/internal/notifications/adapters/postgres"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/application"
	packagingconfig "github.com/jonathanagustin/conduit/internal/packagingworkflow/config"
)

type packagingWorkerRunner func(context.Context, packagingconfig.Config, controlplane.Repository) error

type packagingNotificationSources struct {
	run   func(context.Context)
	close func()
}

type packagingNotificationSetup func(context.Context, func(string) string, controlplane.Repository) (packagingNotificationSources, error)

// The packaging subcommand returns before ordinary Hub assembly. Its source
// repository still needs transaction-bound intent before the first proposal,
// and its IN_APP admission must not depend on an external transport or Hub.
func runPackagingWorker(ctx context.Context, getenv func(string) string, cfg packagingconfig.Config, repository controlplane.Repository, worker packagingWorkerRunner) error {
	return runPackagingWorkerWithNotifications(ctx, getenv, cfg, repository, worker, configurePackagingNotificationSources)
}

func runPackagingWorkerWithNotifications(ctx context.Context, getenv func(string) string, cfg packagingconfig.Config, repository controlplane.Repository, worker packagingWorkerRunner, configure packagingNotificationSetup) error {
	if getenv == nil || repository == nil || worker == nil || configure == nil {
		return errors.New("packaging worker, repository, environment reader and notification setup are required")
	}
	working, cancel := context.WithCancel(ctx)
	sources, err := configure(working, getenv, repository)
	if err != nil {
		cancel()
		return err
	}
	done := make(chan struct{})
	defer func() {
		cancel()
		<-done
		if sources.close != nil {
			sources.close()
		}
	}()
	if sources.run == nil {
		close(done)
	} else {
		go func() {
			defer close(done)
			sources.run(working)
		}()
	}
	return worker(working, cfg, repository)
}

func configurePackagingNotificationSources(ctx context.Context, getenv func(string) string, repository controlplane.Repository) (packagingNotificationSources, error) {
	kind := strings.TrimSpace(getenv("REPOSITORY_KIND"))
	if kind == "" || kind == "file" {
		return packagingNotificationSources{}, nil
	}
	if kind != "postgres" {
		return packagingNotificationSources{}, fmt.Errorf("REPOSITORY_KIND must be file or postgres, got %q", kind)
	}
	dsn, err := readSingleLineSecretFile(getenv("POSTGRES_DSN_FILE"), "POSTGRES_DSN_FILE")
	if err != nil {
		return packagingNotificationSources{}, err
	}
	store, err := notificationpostgres.New(ctx, dsn)
	if err != nil {
		return packagingNotificationSources{}, err
	}
	closeStore := func() { _ = store.Close() }
	eligibility, err := composition.NewNotificationRecipientEligibility(repository, controlplane.HandlerOptions{
		BootstrapAdministrators:              splitCommaSeparated(getenv("PLATFORM_BOOTSTRAP_ADMINISTRATORS")),
		AllowInsecureOperatorIdentityHeaders: getenv("ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS") == "true",
	})
	if err != nil {
		closeStore()
		return packagingNotificationSources{}, err
	}
	if _, err := composition.ConfigureNotificationIntents(repository, store, slog.Default()); err != nil {
		closeStore()
		return packagingNotificationSources{}, err
	}
	config := composition.DefaultIntentDrainerConfig()
	config.OnError = func(err error) {
		if !errors.Is(err, context.Canceled) {
			slog.Error("packaging notification intent admission cycle", "error", err)
		}
	}
	drainer, err := composition.NewNotificationIntentDrainer(store, eligibility, notifications.SystemClock{}, config)
	if err != nil {
		closeStore()
		return packagingNotificationSources{}, err
	}
	return packagingNotificationSources{run: drainer.Run, close: closeStore}, nil
}
