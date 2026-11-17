package composition

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// Named for the composition filter in test-postgres-repository.sh. The real
// producer and stored keys are exercised over independent repository pools;
// membership/claims captured by the subscribing request confer no authority.
func TestPostgresAnnouncementsResolveEnvironmentAliasesBeforeFanoutAndSubmission(t *testing.T) {
	const environment = "alias-dev"
	resource := apolloapplication.EnvironmentRID(environment)
	fixture := newPostgresAnnouncementHub(t, resource)
	seedNotificationEnvironment(t, fixture.repository, environment)
	grant := nativeNotificationViewer(t, fixture.repository, environment, "alice")
	governance := controlplane.NewGovernanceService(fixture.repository)
	if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{
		ID: "alias-review", DisplayName: "Review", Enabled: true, Expression: `require_team("alice-readers", "review")`,
	}); err != nil {
		t.Fatal(err)
	}
	options := controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}
	handler, err := NewHub(fixture.repository, options, Options{NotificationStore: fixture.store})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, http.MethodPut, "/v1/notifications/preferences", "alice",
		fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL","types":["CHANGE_REQUEST_PENDING_APPROVAL"]}`, resource))
	if response.Code != http.StatusOK {
		t.Fatalf("native viewer could not subscribe to the opaque resource: %d %s", response.Code, response.Body)
	}
	var preference domain.Preference
	if err := json.Unmarshal(response.Body.Bytes(), &preference); err != nil || preference.ResourceRID != resource {
		t.Fatalf("stored opaque preference changed: %#v %v", preference, err)
	}
	proposeAliasPlan(t, handler, environment, "pg-alias-pending")
	response = request(t, handler, http.MethodGet, "/v1/notifications/events?resourceRid="+resource, "alice", "")
	if response.Code != http.StatusOK {
		t.Fatalf("native viewer cannot read opaque events: %d %s", response.Code, response.Body)
	}
	inbox, err := fixture.store.Inbox(t.Context(), domain.Recipient{Owner: preference.Owner, Channel: domain.ChannelEmail}, 10)
	if err != nil || len(inbox) != 1 || inbox[0].PreferenceID != preference.ID || inbox[0].Event.ResourceRID != resource {
		t.Fatalf("ordinary recipient did not receive the original opaque event: %#v %v", inbox, err)
	}
	independent, err := controlplane.NewPostgresRepository(t.Context(), fixture.dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer independent.Close()
	eligibility, err := NewNotificationRecipientEligibility(independent, options)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, err := eligibility.Check(t.Context(), preference.Owner, resource); err != nil || !allowed {
		t.Fatalf("independent reader did not resolve the durable identity: %v %v", allowed, err)
	}
	grant.Roles, grant.Operations = nil, []string{controlplane.OperationProposeChange}
	if _, err := controlplane.NewGovernanceService(independent).PutGrant(t.Context(), grant); err != nil {
		t.Fatal(err)
	}
	transport := &notificationSubmissionCapture{}
	config := DefaultDispatcherConfig()
	config.Eligibility = eligibility
	worker, err := NewNotificationDispatcher(fixture.store, transport, &notificationDispatchClock{now: time.Now().UTC()}, config)
	if err != nil {
		t.Fatal(err)
	}
	if found, err := worker.DeliverOne(t.Context()); err != nil || !found {
		t.Fatalf("queued alias authorization = found %v, %v", found, err)
	}
	stats, err := fixture.store.Deliveries(t.Context())
	if err != nil || stats.Delivered != 0 || stats.Exhausted != 1 || len(transport.messages) != 0 {
		t.Fatalf("revoked native grant still authorized external submission: %#v %#v %v", stats, transport.messages, err)
	}
	proposeAliasPlan(t, handler, environment, "pg-alias-revoked")
	after, err := fixture.store.Inbox(t.Context(), domain.Recipient{Owner: preference.Owner, Channel: domain.ChannelEmail}, 10)
	if err != nil || len(after) != 1 || after[0].Event.ID != inbox[0].Event.ID || after[0].PreferenceID != preference.ID {
		t.Fatalf("revocation allowed new fanout or rewrote history: %#v %v", after, err)
	}
}
