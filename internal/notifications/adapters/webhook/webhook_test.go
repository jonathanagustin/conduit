package webhook_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/adapters/webhook"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// stubTransport is the whole reason this adapter was chosen over SMTP: the
// delivery path is exercised to the byte with no socket open and no server
// listening.
type stubTransport struct {
	request  *http.Request
	body     []byte
	status   int
	response string
	err      error
}

func (stub *stubTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	stub.request = request
	if request.Body != nil {
		stub.body, _ = io.ReadAll(request.Body)
	}
	if stub.err != nil {
		return nil, stub.err
	}
	status := stub.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(stub.response)),
		Header:     http.Header{},
	}, nil
}

func message() domain.Message {
	return domain.Message{
		EventID: "nev-1",
		Recipient: domain.Recipient{
			Owner: domain.Owner{Kind: domain.PrincipalTeam, ID: "platform"}, Channel: domain.ChannelEmail,
		},
		Type:        domain.EventPlanFailures,
		ResourceRID: "ri.apollo.main.environment.d41d8cd98f00",
		Environment: "tenant-a",
		Subject:     "[Conduit] PLAN_FAILURES in tenant-a",
		Body:        "Reconciliation gave up",
		OccurredAt:  time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC),
	}
}

func stubbed(t *testing.T, stub *stubTransport) *webhook.Transport {
	t.Helper()
	transport, err := webhook.New("https://hooks.example.com/services/conduit",
		&http.Client{Transport: stub, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func TestASuccessfulPostCarriesTheWholeNotification(t *testing.T) {
	t.Parallel()
	stub := &stubTransport{status: http.StatusAccepted}
	if err := stubbed(t, stub).Deliver(t.Context(), message()); err != nil {
		t.Fatal(err)
	}
	if stub.request.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", stub.request.Method)
	}
	if stub.request.URL.String() != "https://hooks.example.com/services/conduit" {
		t.Errorf("posted to %s", stub.request.URL)
	}
	if got := stub.request.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(stub.body, &payload); err != nil {
		t.Fatalf("the body is not JSON: %v (%s)", err, stub.body)
	}
	for field, want := range map[string]any{
		"version":     "conduit.notification/v1",
		"eventId":     "nev-1",
		"type":        string(domain.EventPlanFailures),
		"resourceRid": "ri.apollo.main.environment.d41d8cd98f00",
		"environment": "tenant-a",
		"subject":     "[Conduit] PLAN_FAILURES in tenant-a",
		"body":        "Reconciliation gave up",
	} {
		if payload[field] != want {
			t.Errorf("payload[%q] = %v, want %v", field, payload[field], want)
		}
	}
	// Slack's incoming webhooks read `text`, so the same POST renders in a
	// channel without a translator in between.
	text, _ := payload["text"].(string)
	if !strings.Contains(text, "[Conduit] PLAN_FAILURES") || !strings.Contains(text, "gave up") {
		t.Errorf("text is not the subject and body a Slack channel would show: %q", text)
	}
	recipient, _ := payload["recipient"].(map[string]any)
	owner, _ := recipient["owner"].(map[string]any)
	if owner["id"] != "platform" || recipient["channel"] != string(domain.ChannelEmail) {
		t.Errorf("the receiver cannot tell who this was for: %v", recipient)
	}
}

// The classification is the part an operator feels. A 4xx spends no budget; a
// 5xx, a 429 and a connection failure all spend one and come back.
func TestTheAnswerDecidesWhetherRetryingCouldEverHelp(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		stub      *stubTransport
		permanent bool
	}{
		"a rejected request": {
			stub: &stubTransport{status: http.StatusNotFound, response: "no such hook"}, permanent: true,
		},
		"an unauthorized request": {
			stub: &stubTransport{status: http.StatusUnauthorized}, permanent: true,
		},
		"a malformed payload the endpoint refuses": {
			stub: &stubTransport{status: http.StatusBadRequest}, permanent: true,
		},
		"a rate limit": {
			stub: &stubTransport{status: http.StatusTooManyRequests}, permanent: false,
		},
		"a request timeout": {
			stub: &stubTransport{status: http.StatusRequestTimeout}, permanent: false,
		},
		"a server error": {
			stub: &stubTransport{status: http.StatusBadGateway}, permanent: false,
		},
		"a connection that never opened": {
			stub: &stubTransport{err: errors.New("dial tcp: connection refused")}, permanent: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := stubbed(t, test.stub).Deliver(t.Context(), message())
			if err == nil {
				t.Fatal("a failed delivery reported success")
			}
			if got := errors.Is(err, ports.ErrPermanentDelivery); got != test.permanent {
				t.Errorf("%s classified permanent=%v, want %v (%v)", name, got, test.permanent, err)
			}
		})
	}
}

// Every 2xx is a success, not just 200: an endpoint answering 204 has taken it.
func TestEverySuccessfulStatusIsAccepted(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent} {
		if err := stubbed(t, &stubTransport{status: status}).Deliver(t.Context(), message()); err != nil {
			t.Errorf("status %d was treated as a failure: %v", status, err)
		}
	}
}

// The endpoint's own words are what an operator reads out of the queue, and one
// failure has to stay one line however much HTML the endpoint answered with.
func TestTheRecordedReasonQuotesTheEndpointAndStaysOneLine(t *testing.T) {
	t.Parallel()
	stub := &stubTransport{
		status:   http.StatusInternalServerError,
		response: "upstream\nexploded\n" + strings.Repeat("x", 4096),
	}
	err := stubbed(t, stub).Deliver(t.Context(), message())
	if err == nil {
		t.Fatal("a 500 reported success")
	}
	if !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("the reason does not quote the endpoint: %q", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("one failure spans several lines: %q", err)
	}
	if len(err.Error()) > 400 {
		t.Errorf("the reason is %d bytes; a verbose endpoint should not fill the column", len(err.Error()))
	}
}

func TestAnEndpointAPostCouldNotReachIsRefusedAtConstruction(t *testing.T) {
	t.Parallel()
	for name, endpoint := range map[string]string{
		"nothing at all":              "   ",
		"a scheme nobody can POST to": "ftp://files.example.com/hook",
		"a path with no host":         "https:///hook",
		"not a URL":                   "://",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := webhook.New(endpoint, nil); err == nil {
				t.Errorf("%s was accepted as a webhook endpoint", name)
			}
		})
	}
}

// A caller that supplies no client gets one with a timeout rather than
// http.DefaultClient, whose absence of one can hold a lease until it expires.
func TestTheDefaultClientHasATimeout(t *testing.T) {
	t.Parallel()
	if _, err := webhook.New("https://hooks.example.com/hook", nil); err != nil {
		t.Fatal(err)
	}
	if webhook.DefaultTimeout <= 0 {
		t.Fatal("the default delivery timeout is not positive")
	}
}

func TestTheTransportNamesItselfForTheOperatorReadingTheQueue(t *testing.T) {
	t.Parallel()
	if name := stubbed(t, &stubTransport{}).Name(); name != "webhook" {
		t.Errorf("Name = %q", name)
	}
}

// The destination travels to the relay, because the relay is the component that
// knows how to reach a mailbox or a Slack channel. It is the one field on this
// wire carrying personal data and this is the one place it is allowed to be.
func TestThePayloadCarriesTheRecipientsDestinationWhenThereIsOne(t *testing.T) {
	t.Parallel()
	stub := &stubTransport{}
	addressed := message()
	addressed.Destination = domain.Destination{
		Kind: domain.ContactSlack, Address: "#platform-alerts",
	}
	if err := stubbed(t, stub).Deliver(t.Context(), addressed); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		Destination *struct {
			Kind    string `json:"kind"`
			Address string `json:"address"`
		} `json:"destination"`
	}
	if err := json.Unmarshal(stub.body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Destination == nil {
		t.Fatalf("the payload named no destination: %s", stub.body)
	}
	if sent.Destination.Kind != string(domain.ContactSlack) ||
		sent.Destination.Address != "#platform-alerts" {
		t.Errorf("the payload named destination %+v", *sent.Destination)
	}
}

// A message naming no destination is delivered exactly as it was before
// contacts existed: no field at all, rather than an empty object a receiver has
// to learn to read as "none". That is what keeps an existing relay working.
func TestAnUnresolvedDestinationIsAbsentFromThePayload(t *testing.T) {
	t.Parallel()
	stub := &stubTransport{}
	if err := stubbed(t, stub).Deliver(t.Context(), message()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stub.body), "destination") {
		t.Errorf("an unaddressed delivery carried a destination field: %s", stub.body)
	}
}

// A failing endpoint's own words end up in the reason an operator reads. The
// address must not, because that reason is stored and logged.
func TestAFailureReasonNeverCarriesTheAddress(t *testing.T) {
	t.Parallel()
	stub := &stubTransport{status: http.StatusInternalServerError, response: "upstream unavailable"}
	addressed := message()
	addressed.Destination = domain.Destination{
		Kind: domain.ContactEmail, Address: "alice@example.test",
	}
	err := stubbed(t, stub).Deliver(t.Context(), addressed)
	if err == nil {
		t.Fatal("a 500 was reported as delivered")
	}
	if strings.Contains(err.Error(), "alice@example.test") {
		t.Fatalf("the failure quoted the address it was delivering to: %v", err)
	}
}
