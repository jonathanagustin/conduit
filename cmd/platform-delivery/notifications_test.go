package main

import (
	"os"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
)

// The promise an existing deployment relies on, checked at the level that
// actually decides it: the Hub's own composition. With no notification
// variable set, configureReconciliation hands main no dispatcher, so no loop
// starts and nothing is sent to anyone who did not ask.
func TestPostgresANotificationTransportIsOffUnlessConfigured(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
	}
	ctx := t.Context()
	base, err := controlplane.NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	dsnFile := writeFile(t, "postgres-dsn", []byte(dsn+"\n"))

	for name, test := range map[string]struct {
		extra    map[string]string
		expected bool
	}{
		"nothing configured": {expected: false},
		"a log transport": {
			extra:    map[string]string{composition.NotificationDeliveryLogVariable: "true"},
			expected: true,
		},
		"a webhook endpoint": {
			extra: map[string]string{
				composition.NotificationWebhookURLVariable: "https://hooks.example.com/conduit",
			},
			expected: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			configuration := map[string]string{"REPOSITORY_KIND": "postgres", "POSTGRES_DSN_FILE": dsnFile}
			for key, value := range test.extra {
				configuration[key] = value
			}
			_, closeReconciliation, _, _, _, deliveries, err := configureReconciliation(ctx,
				func(name string) string { return configuration[name] }, base)
			if err != nil {
				t.Fatal(err)
			}
			defer closeReconciliation()
			if (deliveries != nil) != test.expected {
				t.Fatalf("%s produced a dispatcher = %v, want %v", name, deliveries != nil, test.expected)
			}
		})
	}
}

// A Hub that came up cleanly and silently never told anybody anything is worse
// than one that refused to start, so a contradictory configuration is a startup
// failure and the stores it had already opened are closed on the way out.
func TestPostgresAContradictoryTransportConfigurationStopsTheHub(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
	}
	ctx := t.Context()
	base, err := controlplane.NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	configuration := map[string]string{
		"REPOSITORY_KIND":                           "postgres",
		"POSTGRES_DSN_FILE":                         writeFile(t, "postgres-dsn", []byte(dsn+"\n")),
		composition.NotificationWebhookURLVariable:  "https://hooks.example.com/conduit",
		composition.NotificationDeliveryLogVariable: "true",
	}
	_, closeReconciliation, _, _, _, deliveries, err := configureReconciliation(ctx,
		func(name string) string { return configuration[name] }, base)
	defer closeReconciliation()
	if err == nil {
		t.Fatal("two notification destinations at once were accepted")
	}
	if deliveries != nil {
		t.Error("a refused configuration still produced a dispatcher")
	}
}
