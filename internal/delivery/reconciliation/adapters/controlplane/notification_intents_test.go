package controlplaneadapter

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type notificationIntentHost struct {
	controlplane.Repository
	factory controlplane.TransactionAnnouncementFactory
	err     error
}

func (host *notificationIntentHost) ConfigureNotificationIntents(factory controlplane.TransactionAnnouncementFactory) error {
	host.factory = factory
	return host.err
}

func TestRepositoryForwardsNotificationIntentConfigurationAndItsFailure(t *testing.T) {
	for _, failure := range []error{nil, errors.New("source intent configuration refused")} {
		host := &notificationIntentHost{Repository: controlplane.NewMemoryRepository(), err: failure}
		repository, err := NewRepository(host, newMemoryInputs())
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		err = repository.ConfigureNotificationIntents(func(*sql.Tx) (controlplane.Announcements, error) {
			calls++
			return controlplane.Announcements{}, nil
		})
		if !errors.Is(err, failure) || host.factory == nil {
			t.Fatalf("decorator lost source configuration or its error: %v", err)
		}
		if _, err := host.factory(nil); err != nil || calls != 1 {
			t.Fatal("decorator replaced the caller's source factory", err)
		}
	}
	unsupported, err := NewRepository(controlplane.NewMemoryRepository(), newMemoryInputs())
	if err != nil {
		t.Fatal(err)
	}
	if err := unsupported.ConfigureNotificationIntents(nil); err == nil {
		t.Fatal("decorator pretended its underlying repository supported durable notification intents")
	}
}
