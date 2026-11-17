package composition

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

type notificationAuthorityOutage struct {
	controlplane.Repository
	unavailable bool
}

func (source *notificationAuthorityOutage) ListTeams(ctx context.Context) ([]controlplane.Team, error) {
	if source.unavailable {
		return nil, errors.New("current recipient directory unavailable")
	}
	return source.Repository.ListTeams(ctx)
}

type notificationSubmissionCapture struct{ messages []domain.Message }

func (*notificationSubmissionCapture) Name() string { return "capture" }
func (transport *notificationSubmissionCapture) Deliver(_ context.Context, message domain.Message) error {
	transport.messages = append(transport.messages, message)
	return nil
}

type notificationDispatchClock struct{ now time.Time }

func (clock *notificationDispatchClock) Now() time.Time { return clock.now }

// A real queued event is not permission to send later: another connection may
// revoke native membership after fan-out but before the transport sees it.
// Lookup outages retain the queue's bounded retry path and never count as sent.
func TestPostgresAnnouncementsRecheckAuthorityBeforeExternalDelivery(t *testing.T) {
	for _, scenario := range []string{"membership revoked", "directory recovers"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newPostgresAnnouncementHub(t, "environment/dev-use2")
			options := controlplane.HandlerOptions{
				AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
			}
			governance := controlplane.NewGovernanceService(fixture.repository)
			team, err := governance.PutTeam(t.Context(), controlplane.Team{
				Name: "readers", DisplayName: "Readers", AdminUsers: []string{"root"}, MemberUsers: []string{"alice"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{
				ResourceRID: fixture.resource, Team: "readers", Operations: []string{controlplane.OperationReadResource},
			}); err != nil {
				t.Fatal(err)
			}
			handler, err := NewHub(fixture.repository, options, Options{NotificationStore: fixture.store})
			if err != nil {
				t.Fatal(err)
			}
			preference := request(t, handler, http.MethodPut, "/v1/notifications/preferences", "alice",
				fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL","types":["ROLE_CHANGE"]}`, fixture.resource))
			if preference.Code != http.StatusOK {
				t.Fatalf("subscribe = %d %s", preference.Code, preference.Body)
			}
			written := request(t, handler, http.MethodPost, "/v1/resource-grants", "root",
				fmt.Sprintf(`{"resourceRid":%q,"team":"reviewers","operations":["resource:read"]}`, fixture.resource))
			if written.Code != http.StatusOK {
				t.Fatalf("raise event = %d %s", written.Code, written.Body)
			}
			fixture.admit(t, options)
			stats, err := fixture.store.Deliveries(t.Context())
			if err != nil || stats.Pending != 1 {
				t.Fatalf("fixture did not queue one eligible recipient: %#v %v", stats, err)
			}
			// Read current authority through an independent repository pool.
			independent, err := controlplane.NewPostgresRepository(t.Context(), fixture.dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer independent.Close()
			authority := &notificationAuthorityOutage{Repository: independent}
			eligibility, err := NewNotificationRecipientEligibility(authority, options)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "membership revoked" {
				team.MemberUsers = []string{"replacement"}
				if _, err := governance.PutTeam(t.Context(), team); err != nil {
					t.Fatal(err)
				}
			} else {
				authority.unavailable = true
			}
			transport := &notificationSubmissionCapture{}
			clock := &notificationDispatchClock{now: time.Now().UTC()}
			config := DefaultDispatcherConfig()
			config.Eligibility = eligibility
			worker, err := NewNotificationDispatcher(fixture.store, transport, clock, config)
			if err != nil {
				t.Fatal(err)
			}
			if found, err := worker.DeliverOne(t.Context()); err != nil || !found {
				t.Fatalf("queued authority check = found %v, %v", found, err)
			}
			stats, err = fixture.store.Deliveries(t.Context())
			if err != nil || len(transport.messages) != 0 || stats.Delivered != 0 {
				t.Fatalf("revoked or unknown recipient was sent: %#v messages=%#v %v", stats, transport.messages, err)
			}
			if scenario == "membership revoked" {
				if stats.Exhausted != 1 || stats.Pending != 0 {
					t.Fatalf("definitive refusal was not terminal: %#v", stats)
				}
			} else {
				if stats.Pending != 1 || stats.Exhausted != 0 {
					t.Fatalf("lookup outage was not left retryable: %#v", stats)
				}
				authority.unavailable = false
				clock.now = clock.now.Add(3 * time.Second)
				if found, err := worker.DeliverOne(t.Context()); err != nil || !found {
					t.Fatalf("retry after recovery = found %v, %v", found, err)
				}
				if len(transport.messages) != 1 {
					t.Fatalf("recovered current authority was not submitted: %#v", transport.messages)
				}
			}
			// Historical inbox retention is unchanged; these checks govern new
			// fan-out and submission, not deletion of an existing personal record.
			inbox, err := fixture.store.Inbox(t.Context(), domain.Recipient{
				Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, Channel: domain.ChannelEmail,
			}, 10)
			if err != nil || len(inbox) != 1 || !strings.Contains(inbox[0].Event.Summary, "reviewers") {
				t.Fatalf("authority check rewrote the historical inbox: %#v %v", inbox, err)
			}
		})
	}
}
