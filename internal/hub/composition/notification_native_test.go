package composition_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func credential(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

type nativeContacts struct{ contacts []domain.Contact }

func (*nativeContacts) PreferencesFor(context.Context, string) ([]domain.Preference, error) {
	return nil, nil
}

func (*nativeContacts) SaveContact(context.Context, domain.Contact) error { return nil }
func (store *nativeContacts) ContactsFor(context.Context, domain.Owner) ([]domain.Contact, error) {
	return store.contacts, nil
}
func (*nativeContacts) DeleteContact(context.Context, domain.Owner, domain.ContactKind) (bool, error) {
	return false, nil
}

func TestNativeNotificationChannelsAndContactScopeAreWired(t *testing.T) {
	variables := map[string]string{
		composition.NotificationSMTPAddressVariable:    "mail.example.test:587",
		composition.NotificationSMTPFromVariable:       "sender@example.test",
		composition.NotificationSlackTokenFileVariable: credential(t, "xoxb-test-secret\n"),
	}
	transport, err := composition.NewNotificationTransport(environment(variables), nil)
	if err != nil || transport.Name() != "native" {
		t.Fatalf("native transport=%v, %v", transport, err)
	}
	owner := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	store := &nativeContacts{contacts: []domain.Contact{{Owner: owner, Kind: domain.ContactSlack, Address: "#private-channel"}}}
	resolver, err := composition.NewNotificationDestinationResolver(transport, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := resolver.DestinationFor(t.Context(), domain.Recipient{Owner: owner, Channel: domain.ChannelEmail}); err != nil || found {
		t.Fatalf("native email fell back to Slack: found=%v, error=%v", found, err)
	}
	destination, found, err := resolver.DestinationFor(t.Context(), domain.Recipient{Owner: owner, Channel: domain.ChannelSlack})
	if err != nil || !found || destination.Kind != domain.ContactSlack {
		t.Fatalf("Slack contact unresolved: %v", err)
	}
	legacy, err := composition.NewNotificationTransport(environment(map[string]string{composition.NotificationWebhookURLVariable: "https://hooks.example.test/"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	relay, err := composition.NewNotificationDestinationResolver(legacy, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := relay.DestinationFor(t.Context(), domain.Recipient{Owner: owner, Channel: domain.ChannelEmail}); err != nil || !found {
		t.Fatal("generic relay lost its legacy contact resolution")
	}
}

func TestPartialNativeConfigurationFailsWithoutDisclosingSecrets(t *testing.T) {
	token := credential(t, "xoxb-test-secret")
	for _, variables := range []map[string]string{
		{composition.NotificationSMTPFromVariable: "sender@example.test"},
		{composition.NotificationSMTPAddressVariable: "mail.example.test:587"},
		{composition.NotificationSlackTokenFileVariable: "missing-secret-file"},
		{composition.NotificationSlackTokenFileVariable: credential(t, "")},
		{composition.NotificationSlackTokenFileVariable: token, composition.NotificationNativeTimeoutVariable: "30s"},
		{composition.NotificationSlackTokenFileVariable: token, composition.NotificationNativeTimeoutVariable: "0s"},
		{composition.NotificationSlackTokenFileVariable: token, composition.NotificationNativeTimeoutVariable: "secret-invalid-duration"},
		{composition.NotificationSlackTokenFileVariable: token, composition.NotificationWebhookURLVariable: "https://hooks.example.test/"},
		{composition.NotificationSlackTokenFileVariable: token, composition.NotificationDeliveryLogVariable: "true"},
	} {
		transport, err := composition.NewNotificationTransport(environment(variables), nil)
		if err == nil || transport != nil {
			t.Fatal("incomplete/conflicting delivery configuration accepted")
		}
		if strings.Contains(err.Error(), "xoxb-test-secret") || strings.Contains(err.Error(), "secret-invalid-duration") {
			t.Fatal("configuration error disclosed input credentials")
		}
	}
}
