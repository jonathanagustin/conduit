package composition

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func TestRevokedSubscriptionsCannotReadNewResourceEvents(t *testing.T) {
	for _, mode := range []string{"personal subscription after team membership removed", "team subscription after resource read grant removed"} {
		t.Run(mode, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			governance := controlplane.NewGovernanceService(repository)
			team, err := governance.PutTeam(t.Context(), controlplane.Team{
				Name: "readers", DisplayName: "Readers", AdminUsers: []string{"root"}, MemberUsers: []string{"alice"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := governance.PutTeam(t.Context(), controlplane.Team{
				Name: "operators", DisplayName: "Operators", AdminUsers: []string{"root"}, MemberUsers: []string{"operator"},
			}); err != nil {
				t.Fatal(err)
			}
			grant, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{
				ResourceRID: environmentRID, Team: "readers", Operations: []string{controlplane.OperationReadResource},
			})
			if err != nil {
				t.Fatal(err)
			}
			store := newNotificationStore()
			handler, err := NewHub(repository, controlplane.HandlerOptions{
				AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
			}, Options{NotificationStore: store})
			if err != nil {
				t.Fatal(err)
			}
			ownerFields := ""
			ownerQuery := ""
			if strings.HasPrefix(mode, "team") {
				ownerFields = `,"owner":{"kind":"TEAM","id":"readers"},"contactId":"contact-readers"`
				ownerQuery = "&ownerKind=TEAM&ownerId=readers"
			}
			preference := request(t, handler, http.MethodPut, "/v1/notifications/preferences", "alice",
				fmt.Sprintf(`{"resourceRid":%q,"channel":"IN_APP","types":["ROLE_CHANGE"]%s}`, environmentRID, ownerFields))
			if preference.Code != http.StatusOK {
				t.Fatalf("subscribe while authorized = %d %s", preference.Code, preference.Body)
			}
			writeGrant := func(operations []string, revision uint64) controlplane.ResourceGrant {
				body, err := json.Marshal(controlplane.ResourceGrant{ResourceRID: environmentRID, Team: "operators", Operations: operations, Revision: revision})
				if err != nil {
					t.Fatal(err)
				}
				response := request(t, handler, http.MethodPost, "/v1/resource-grants", "root", string(body))
				if response.Code != http.StatusOK {
					t.Fatalf("source grant = %d %s", response.Code, response.Body)
				}
				var stored controlplane.ResourceGrant
				if err := json.Unmarshal(response.Body.Bytes(), &stored); err != nil {
					t.Fatal(err)
				}
				return stored
			}
			first := writeGrant([]string{controlplane.OperationReadResource}, 0)
			readInbox := func() []domain.InboxEntry {
				response := request(t, handler, http.MethodGet, "/v1/notifications/inbox?channel=IN_APP"+ownerQuery, "alice", "")
				if response.Code != http.StatusOK {
					t.Fatalf("inbox = %d %s", response.Code, response.Body)
				}
				var body struct {
					Entries []domain.InboxEntry `json:"entries"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				return body.Entries
			}
			before := readInbox()
			if len(before) != 1 {
				t.Fatalf("fixture historical inbox = %#v", before)
			}
			if strings.HasPrefix(mode, "personal") {
				team.MemberUsers = []string{"replacement"}
				if _, err := governance.PutTeam(t.Context(), team); err != nil {
					t.Fatal(err)
				}
			} else {
				grant.Operations = []string{controlplane.OperationProposeChange}
				if _, err := governance.PutGrant(t.Context(), grant); err != nil {
					t.Fatal(err)
				}
			}
			denied := request(t, handler, http.MethodGet, "/v1/notifications/events?resourceRid="+environmentRID, "alice", "")
			if denied.Code != http.StatusForbidden {
				t.Fatalf("fixture access not revoked: %d %s", denied.Code, denied.Body)
			}
			// This event is a new source mutation after revocation, not a replay
			// or an already-delivered historical inbox entry.
			writeGrant([]string{controlplane.OperationOperate}, first.Revision)
			for _, entry := range readInbox() {
				if entry.Event.ID != before[0].Event.ID {
					t.Fatalf("after revocation, resource events return 403 but own inbox reveals NEW event: %s", entry.Event.Summary)
				}
			}
		})
	}
}
