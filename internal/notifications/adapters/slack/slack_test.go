package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (trip roundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return trip(request) }
func message() domain.Message {
	return domain.Message{Recipient: domain.Recipient{Channel: domain.ChannelSlack}, Destination: domain.Destination{Kind: domain.ContactSlack, Address: "C-private"}, Subject: "Release recalled", Body: "Withdrawn <@channel> & reviewed"}
}

func TestSlackPostsToTheRecipientsChannelAndRequiresAPIAcknowledgement(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodPost || request.URL.String() != endpoint || request.Header.Get("Authorization") != "Bearer xoxb-test-secret" {
			t.Fatal("incorrect Slack request authority or endpoint")
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["channel"] != "C-private" || payload["text"] != "Release recalled\n\nWithdrawn &lt;@channel&gt; &amp; reviewed" || payload["mrkdwn"] != false || payload["unfurl_links"] != false || payload["unfurl_media"] != false {
			t.Fatalf("payload=%#v", payload)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})}
	transport, err := New("xoxb-test-secret", client)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.Deliver(context.Background(), message()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("requests=%d", calls)
	}
	if strings.Contains(fmt.Sprint(transport), "xoxb-test-secret") {
		t.Fatal("transport formatting disclosed its token")
	}
	if transport.client.Timeout <= 0 {
		t.Fatal("injected client retained an unbounded timeout")
	}
}

func TestSlackFailuresAreClassifiedWithoutDisclosingResponseOrContact(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		body      string
		permanent bool
	}{
		{"logical rejection", 200, `{"ok":false,"error":"invalid_auth"}`, true},
		{"unknown rejection", 200, `{"ok":false,"error":"C-private xoxb-test-secret"}`, false},
		{"server outage", 503, "C-private xoxb-test-secret", false},
		{"unauthorized", 401, "C-private xoxb-test-secret", true},
		{"malformed ack", 200, `not-json C-private xoxb-test-secret`, false},
		{"no affirmative ack", 200, `{}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, err := New("xoxb-test-secret", &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			err = transport.Deliver(context.Background(), message())
			if err == nil || errors.Is(err, ports.ErrPermanentDelivery) != test.permanent {
				t.Fatalf("failure=%v, permanent=%v", err, test.permanent)
			}
			if strings.Contains(err.Error(), "C-private") || strings.Contains(err.Error(), "xoxb-test-secret") {
				t.Fatal("delivery error disclosed a response, contact or credential")
			}
		})
	}
}

func TestSlackRateLimitCarriesTheProviderMinimumDelay(t *testing.T) {
	transport, _ := New("xoxb-test-secret", &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Retry-After": []string{"45"}}}, nil
	})})
	err := transport.Deliver(context.Background(), message())
	var delayed ports.RetryAfterError
	if !errors.As(err, &delayed) || delayed.RetryAfter() != 45*time.Second || errors.Is(err, ports.ErrPermanentDelivery) {
		t.Fatalf("rate limit=%v", err)
	}
}

func TestSlackRefusesRedirectsAndInvalidContactBeforeSending(t *testing.T) {
	calls := 0
	transport, _ := New("xoxb-test-secret", &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://elsewhere.example/"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})})
	if err := transport.Deliver(context.Background(), message()); !errors.Is(err, ports.ErrPermanentDelivery) {
		t.Fatalf("redirect=%v", err)
	}
	if calls != 1 {
		t.Fatal("a redirect forwarded the request")
	}
	invalid := message()
	invalid.Destination.Kind = domain.ContactEmail
	if err := transport.Deliver(context.Background(), invalid); !errors.Is(err, ports.ErrPermanentDelivery) {
		t.Fatalf("wrong contact=%v", err)
	}
	if calls != 1 {
		t.Fatal("a wrong contact was sent")
	}
	for _, token := range []string{"", "xoxb-\nsecret", "xoxb- secret"} {
		if _, err := New(token, nil); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}
