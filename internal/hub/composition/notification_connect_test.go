package composition

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	notificationsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/notifications/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/encoding/protojson"
)

const notificationEventProcedure = "/conduit.notifications.v1.NotificationService/ListNotificationEvents"

func TestNotificationConnectRetainsCarrierAuthorization(t *testing.T) {
	handler, _, resource := delegationHub(t)
	for _, test := range []struct {
		actor, represented string
		status             int
	}{{"alice", "", 200}, {"alice", "bob", 200}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403}} {
		t.Run(test.actor+"/"+test.represented, func(t *testing.T) {
			native := as(t, handler, "/v1/notifications/events?resourceRid="+url.QueryEscape(resource), test.actor, test.represented)
			body, _ := json.Marshal(map[string]string{"resourceRid": resource})
			request := httptest.NewRequest(http.MethodPost, notificationEventProcedure, strings.NewReader(string(body)))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Connect-Protocol-Version", "1")
			request.Header.Set("X-Platform-Subject", test.actor)
			if test.represented != "" {
				request.Header.Set("X-Platform-Service-Account", "true")
				request.Header.Set("X-Platform-On-Behalf-Of", test.represented)
			}
			wire := httptest.NewRecorder()
			handler.ServeHTTP(wire, request)
			if native.Code != test.status || wire.Code != native.Code {
				t.Fatalf("REST=%d RPC=%d expected=%d body=%s", native.Code, wire.Code, test.status, wire.Body.String())
			}
			if wire.Code != 200 {
				if strings.Contains(wire.Body.String(), "confidential body") {
					t.Fatal("forbidden carrier disclosed an event")
				}
				return
			}
			converted := &notificationsv1.ListNotificationEventsResponse{}
			if err := protojson.Unmarshal(wire.Body.Bytes(), converted); err != nil {
				t.Fatal(err)
			}
			var original struct {
				ResourceRID string                         `json:"resourceRid"`
				Limit       int                            `json:"limit"`
				Events      []struct{ ID, Summary string } `json:"events"`
			}
			if err := json.Unmarshal(native.Body.Bytes(), &original); err != nil {
				t.Fatal(err)
			}
			if converted.GetResourceRid() != original.ResourceRID || int(converted.GetLimit()) != original.Limit || len(converted.Events) != len(original.Events) || len(converted.Events) != 1 {
				t.Fatal("native event scope changed")
			}
			if converted.Events[0].GetId() != original.Events[0].ID || converted.Events[0].GetSummary() != original.Events[0].Summary {
				t.Fatal("native event body changed")
			}
		})
	}
}
func TestNotificationConnectDoesNotInventAnUnconfiguredStoreOrMethod(t *testing.T) {
	for _, storeConfigured := range []bool{false, true} {
		options := Options{}
		if storeConfigured {
			options.NotificationStore = newNotificationStore()
		}
		handler, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{}, options)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, notificationEventProcedure, strings.NewReader(`{"resourceRid":"environment/private"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Connect-Protocol-Version", "1")
		request.Header.Set("X-Platform-Subject", "forged-admin")
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, request)
		expected := 404
		if storeConfigured {
			expected = 401
		}
		if result.Code != expected {
			t.Fatalf("configured=%v status=%d expected=%d", storeConfigured, result.Code, expected)
		}
		for _, path := range []string{"/conduit.notifications.v1.NotificationService/Subscribe", "/conduit.notifications.v1.NotificationService/EmitEvent"} {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
			if response.Code != 404 {
				t.Fatalf("unregistered method served: %s status=%d", path, response.Code)
			}
		}
	}
}
