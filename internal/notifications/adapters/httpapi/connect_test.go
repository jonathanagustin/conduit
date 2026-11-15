package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	notificationsv1 "github.com/jonathanagustin/conduit/gen/go/conduit/notifications/v1"
	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func eventRPC(handler http.Handler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, strings.TrimPrefix(ConnectRoute, "POST "), strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestConnectEventReadMatchesRESTAndPreservesOptionalLimit(t *testing.T) {
	harness := newHarness(t, Principal{Username: "alice"})
	harness.store.events = []domain.Event{
		{ID: "first", Type: domain.EventPlanFailures, ResourceRID: resource, Environment: "east", Summary: "review <untrusted>", Actor: "operator", OccurredAt: at().Add(123456789 * time.Nanosecond)},
		{ID: "second", Type: domain.EventPlanFailures, ResourceRID: resource, Summary: "system event", OccurredAt: at()},
		{ID: "foreign", Type: domain.EventPlanFailures, ResourceRID: "environment/foreign", Summary: "private", OccurredAt: at()},
	}
	for _, test := range []struct {
		name, limit string
		status      int
	}{{"absent", "", 200}, {"one", "1", 200}, {"ceiling", "9223372036854775807", 200}, {"zero", "0", 400}, {"negative", "-1", 400}} {
		t.Run(test.name, func(t *testing.T) {
			target := "/v1/notifications/events?resourceRid=" + url.QueryEscape("  "+resource+"  ")
			body := map[string]any{"resourceRid": "  " + resource + "  "}
			if test.limit != "" {
				target += "&limit=" + url.QueryEscape(test.limit)
				body["limit"] = test.limit
			}
			encoded, _ := json.Marshal(body)
			native := harness.do(http.MethodGet, target, "")
			converted := eventRPC(harness.handler, string(encoded))
			if native.Code != test.status || converted.Code != test.status {
				t.Fatalf("REST=%d RPC=%d expected=%d body=%s", native.Code, converted.Code, test.status, converted.Body.String())
			}
			if converted.Code != 200 {
				return
			}
			var page eventPage
			if err := json.Unmarshal(native.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			wire := &notificationsv1.ListNotificationEventsResponse{}
			if err := protojson.Unmarshal(converted.Body.Bytes(), wire); err != nil {
				t.Fatal(err)
			}
			if wire.GetResourceRid() != page.ResourceRID || int(wire.GetLimit()) != page.Limit || len(wire.Events) != len(page.Events) {
				t.Fatalf("envelope changed: native=%+v wire=%+v", page, wire)
			}
			for index, row := range wire.Events {
				original := page.Events[index]
				if row.GetId() != original.ID || row.GetType() != string(original.Type) || row.GetResourceRid() != original.ResourceRID || row.GetEnvironment() != original.Environment || row.GetSummary() != original.Summary || row.GetActor() != original.Actor || row.GetOccurredAt() == nil || !row.GetOccurredAt().AsTime().Equal(original.OccurredAt) {
					t.Fatalf("row changed: native=%+v wire=%+v", original, row)
				}
			}
		})
	}
	for _, body := range []string{`{}`, `{"resourceRid":"  "}`} {
		if response := eventRPC(harness.handler, body); response.Code != 400 {
			t.Fatalf("missing resource answered %d", response.Code)
		}
	}
	harness.denyRID[resource] = true
	if response := eventRPC(harness.handler, `{"resourceRid":"`+resource+`","limit":"0"}`); response.Code != 403 {
		t.Fatalf("invalid limit bypassed resource check: %d", response.Code)
	}
}

type eventObservedBody struct {
	io.Reader
	reads int
}

func (body *eventObservedBody) Read(buffer []byte) (int, error) {
	body.reads++
	return body.Reader.Read(buffer)
}
func TestConnectEventReadRefusesIdentityBeforeBody(t *testing.T) {
	for _, test := range []struct {
		name      string
		principal Principal
		err       error
		status    int
	}{{name: "anonymous", err: errors.New("no credential"), status: 401}, {name: "blank", status: 401}, {name: "delegation", err: StatusError{Status: 403, Message: "delegation denied"}, status: 403}} {
		t.Run(test.name, func(t *testing.T) {
			harness := newHarness(t, test.principal)
			harness.resolve = func(*http.Request) (Principal, error) { return test.principal, test.err }
			body := &eventObservedBody{Reader: strings.NewReader(`not JSON`)}
			request := httptest.NewRequest(http.MethodPost, strings.TrimPrefix(ConnectRoute, "POST "), body)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			harness.handler.ServeHTTP(response, request)
			if response.Code != test.status || body.reads != 0 || harness.store.lastLimit != 0 {
				t.Fatalf("status=%d bodyReads=%d storeLimit=%d", response.Code, body.reads, harness.store.lastLimit)
			}
		})
	}
}

type brokenEventStore struct {
	ports.Store
	rows []domain.Event
	err  error
}

func (store brokenEventStore) EventsForResource(context.Context, string, int) ([]domain.Event, error) {
	return store.rows, store.err
}
func TestBothEventTransportsRejectCrossResourceOrUnboundedStoreOutput(t *testing.T) {
	for _, test := range []struct {
		name string
		rows []domain.Event
		err  error
	}{{name: "wrong resource", rows: []domain.Event{{ID: "foreign-secret", ResourceRID: "environment/foreign", OccurredAt: at()}}}, {name: "oversized", rows: []domain.Event{{ID: "one", ResourceRID: resource, OccurredAt: at()}, {ID: "two", ResourceRID: resource, OccurredAt: at()}}}, {name: "backend failure", err: errors.New("sensitive database detail")}} {
		t.Run(test.name, func(t *testing.T) {
			store := newStore()
			reader, err := application.NewReader(brokenEventStore{Store: store, rows: test.rows, err: test.err})
			if err != nil {
				t.Fatal(err)
			}
			preferences, err := application.NewPreferences(store, fixedAt{})
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			if err := Register(mux, Config{Reader: reader, Preferences: preferences, Resolve: func(*http.Request) (Principal, error) { return Principal{Username: "alice"}, nil }, Authorize: func(context.Context, Principal, string, string) bool { return true }}); err != nil {
				t.Fatal(err)
			}
			native := httptest.NewRecorder()
			mux.ServeHTTP(native, httptest.NewRequest(http.MethodGet, "/v1/notifications/events?resourceRid="+url.QueryEscape(resource)+"&limit=1", nil))
			wire := eventRPC(mux, `{"resourceRid":"`+resource+`","limit":"1"}`)
			if native.Code != 500 || wire.Code != 500 {
				t.Fatalf("REST=%d RPC=%d", native.Code, wire.Code)
			}
			if strings.Contains(wire.Body.String(), "foreign-secret") || strings.Contains(wire.Body.String(), "sensitive database detail") {
				t.Fatal("bad source details leaked through Connect")
			}
		})
	}
}

func TestNotificationWireFieldsMatchNativeEventAndEnvelope(t *testing.T) {
	for _, pair := range []struct {
		native any
		wire   proto.Message
	}{{domain.Event{}, &notificationsv1.NotificationEvent{}}, {eventPage{}, &notificationsv1.ListNotificationEventsResponse{}}} {
		source := reflect.TypeOf(pair.native)
		var native, wire []string
		for index := 0; index < source.NumField(); index++ {
			name := strings.Split(source.Field(index).Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				native = append(native, name)
			}
		}
		fields := pair.wire.ProtoReflect().Descriptor().Fields()
		for index := 0; index < fields.Len(); index++ {
			wire = append(wire, fields.Get(index).JSONName())
		}
		sort.Strings(native)
		sort.Strings(wire)
		if !reflect.DeepEqual(native, wire) {
			t.Fatalf("%s native fields=%v wire fields=%v", source, native, wire)
		}
	}
	field := (&notificationsv1.ListNotificationEventsRequest{}).ProtoReflect().Descriptor().Fields().ByName("limit")
	if !field.HasPresence() {
		t.Fatal("absent and explicit-zero limits must stay distinct")
	}
}
