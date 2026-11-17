package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func TestPreferenceOptOutRoundTripsAndStillRequiresOwnerAndResourceAuthority(t *testing.T) {
	test := newHarness(t, alice())
	put := func(types string) *preferenceResponse {
		t.Helper()
		response := test.do(http.MethodPut, "/v1/notifications/preferences", fmt.Sprintf(
			`{"resourceRid":%q,"channel":"EMAIL","types":%s,"allFutureTypes":false,"contactId":"chosen-contact"}`, resource, types))
		if response.Code != http.StatusOK {
			t.Fatalf("save %s = %d %s", types, response.Code, response.Body)
		}
		var body preferenceResponse
		decode(t, response, &body)
		return &body
	}
	on := put(`["PLAN_FAILURES"]`)
	off := put(`[]`)
	if off.ID != on.ID || off.Revision != on.Revision+1 || len(off.Types) != 0 || off.AllFutureTypes || off.ContactID != "chosen-contact" {
		t.Fatalf("off response lost tuple/consent: %#v", off)
	}
	read := test.do(http.MethodGet, "/v1/notifications/preferences?resourceRid="+resource+"&channel=EMAIL", "")
	if read.Code != http.StatusOK {
		t.Fatalf("read off = %d %s", read.Code, read.Body)
	}
	var loaded preferenceResponse
	decode(t, read, &loaded)
	if loaded.ID != off.ID || len(loaded.Types) != 0 || loaded.AllFutureTypes || loaded.TypeAvailabilityScope != "ALL_KNOWN" {
		t.Fatalf("loaded off response = %#v", loaded)
	}
	resubscribed := put(`["PLAN_FAILURES"]`)
	if resubscribed.Revision != off.Revision+1 {
		t.Fatalf("resubscribe did not advance stored off revision: %#v", resubscribed)
	}
	test.denyRID[resource] = true
	denied := test.do(http.MethodPut, "/v1/notifications/preferences", fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL","types":[]}`, resource))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("off consent bypassed resource authorization: %d %s", denied.Code, denied.Body)
	}
	test.denyRID[resource] = false
	foreign := test.do(http.MethodPut, "/v1/notifications/preferences", fmt.Sprintf(`{"resourceRid":%q,"owner":{"kind":"USER","id":"bob"},"channel":"EMAIL","types":[]}`, resource))
	if foreign.Code != http.StatusForbidden {
		t.Fatalf("off consent bypassed owner authorization: %d %s", foreign.Code, foreign.Body)
	}
}

func TestTeamOptOutNeedsNoContactButActiveConsentStillDoes(t *testing.T) {
	test := newHarness(t, alice())
	for _, contact := range []string{"", "deleted-contact"} {
		response := test.do(http.MethodPut, "/v1/notifications/preferences", fmt.Sprintf(
			`{"resourceRid":%q,"owner":{"kind":"TEAM","id":"platform"},"channel":"EMAIL","types":[],"contactId":%q}`, resource, contact))
		if response.Code != http.StatusOK {
			t.Fatalf("team opt-out with contact %q = %d %s", contact, response.Code, response.Body)
		}
		var off preferenceResponse
		decode(t, response, &off)
		if off.ContactID != contact || len(off.Types) != 0 || off.AllFutureTypes || off.Owner.Kind != domain.PrincipalTeam {
			t.Fatalf("team off response = %#v", off)
		}
	}
	active := test.do(http.MethodPut, "/v1/notifications/preferences", fmt.Sprintf(`{"resourceRid":%q,"owner":{"kind":"TEAM","id":"platform"},"channel":"EMAIL","types":["PLAN_FAILURES"]}`, resource))
	if active.Code != http.StatusUnprocessableEntity {
		t.Fatalf("active team consent without contact = %d %s", active.Code, active.Body)
	}
}
