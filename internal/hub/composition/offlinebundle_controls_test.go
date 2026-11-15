package composition

import (
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"net/http"
	"testing"
	"time"
)

func TestOfflineBundlePresenceUsesFreshStoredAgentReport(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC)
	presence := sitePresence{repository: repository, now: func() time.Time { return now }}
	for _, test := range []struct {
		name   string
		report controlplane.EnvironmentReport
		want   bool
	}{
		{name: "missing"},
		{name: "no agent", report: controlplane.EnvironmentReport{ObservedAt: now}},
		{name: "zero time", report: controlplane.EnvironmentReport{AgentID: "agent"}},
		{name: "stale", report: controlplane.EnvironmentReport{AgentID: "agent", ObservedAt: now.Add(-time.Minute)}},
		{name: "fresh", report: controlplane.EnvironmentReport{AgentID: "agent", ObservedAt: now.Add(-time.Second)}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.report.Environment = test.name
			if test.name != "missing" {
				if _, err := repository.PutEnvironmentReport(t.Context(), test.report); err != nil {
					t.Fatal(err)
				}
			}
			online, err := presence.Online(t.Context(), test.name)
			if err != nil || online != test.want {
				t.Fatalf("online=%v err=%v want=%v", online, err, test.want)
			}
		})
	}
}

func TestOfflineBundleOperatorCannotAssertPresenceOrBypassGrant(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		actor, body string
		want        int
	}{
		{"nobody", `{"action":"handoff","confirm":true}`, http.StatusForbidden},
		{"root", `{"action":"report","online":true}`, http.StatusBadRequest},
		{"root", `{"action":"report"} {}`, http.StatusBadRequest},
	} {
		response := postAssistant(handler, test.actor, "", "/v1/offline-bundles", test.body)
		if response.Code != test.want {
			t.Fatalf("%s %s: %d %s", test.actor, test.body, response.Code, response.Body.String())
		}
	}
}
