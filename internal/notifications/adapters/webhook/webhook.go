// Package webhook delivers a rendered notification by POSTing it to one
// operator-configured HTTPS endpoint.
//
// Why a webhook rather than SMTP. Both were candidates and the deciding
// question was what Conduit can honestly promise. SMTP needs an envelope
// sender, a TLS and authentication policy, and a recipient address -- and
// nothing in this repository stores a recipient address: domain.Contact models
// one, no store persists one, and inventing an address book would be inventing
// Apollo behaviour rather than delivering a notification. A webhook needs one
// URL, which an operator already has: a Slack incoming webhook is exactly this
// shape, and so is every mail relay's HTTP submission API. It is also the
// adapter that can be tested to the byte with no socket open, by substituting
// an http.RoundTripper, which SMTP cannot be without a fake server.
//
// What is claimed and what is not: Apollo's own settings pages establish that a
// team may own a Slack contact and that Slack contacts are deliverable. Nothing
// establishes how Apollo posts to Slack or what it puts in the body, so the
// payload below is a Conduit extension and is declared as one.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// DefaultTimeout bounds one delivery attempt. A transport with no timeout can
// hold a lease until it expires and then hand the same entry to another
// replica, which turns one slow endpoint into a growing pile of concurrent
// in-flight requests to the same slow endpoint.
const DefaultTimeout = 10 * time.Second

// Transport POSTs a message as JSON.
type Transport struct {
	endpoint string
	client   *http.Client
}

var _ ports.Transport = (*Transport)(nil)

// New refuses an endpoint a POST could not reach.
//
// The client is injected rather than constructed here, which is what makes this
// testable with no network: a test supplies an http.Client whose Transport is a
// stub RoundTripper and asserts on the request that would have been sent. A nil
// client is the production path and gets one with DefaultTimeout, because a
// caller that forgot the timeout should not get http.DefaultClient's absence of
// one.
func New(endpoint string, client *http.Client) (*Transport, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("a notification webhook endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse notification webhook endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf(
			"notification webhook endpoint must be http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, errors.New("notification webhook endpoint names no host")
	}
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return &Transport{endpoint: endpoint, client: client}, nil
}

func (transport *Transport) Name() string { return "webhook" }

// payload is the wire shape. Flat, self-describing, and carrying the identity
// the delivery is keyed on, so a receiver that has already seen this event for
// this recipient can drop it -- which it has to be able to do, because the queue
// behind this is at-least-once.
//
// CONDUIT-INVENTED: notifications.WebhookPayload
// Displaced-by: a capture of the request Apollo makes when it delivers to a
// Slack or email team contact, including its field names and headers
type payload struct {
	// Version is first so a receiver can branch on it before reading anything
	// else, and so adding a field later is not indistinguishable from a bug.
	Version     string           `json:"version"`
	EventID     string           `json:"eventId"`
	Type        domain.EventType `json:"type"`
	ResourceRID string           `json:"resourceRid"`
	Environment string           `json:"environment,omitempty"`
	Recipient   domain.Recipient `json:"recipient"`
	Subject     string           `json:"subject"`
	// Text is the field Slack's incoming webhooks read, so the same POST that
	// feeds a generic receiver renders in a Slack channel without a translator
	// in between. The body is repeated under its own name for receivers that do
	// not know Slack's vocabulary.
	Text       string    `json:"text"`
	Body       string    `json:"body"`
	OccurredAt time.Time `json:"occurredAt"`
}

const payloadVersion = "conduit.notification/v1"

// Deliver POSTs the message and classifies the answer.
//
// The classification is the part that matters. A 2xx is delivered. A 4xx other
// than 408 and 429 is permanent -- the endpoint understood the request and
// rejected it, and it will reject the identical request an hour from now, so
// the entry becomes terminal immediately with the status recorded rather than
// spending a ten-attempt budget to reach the same place. 408 and 429 are the
// two 4xx codes that mean "not now" rather than "not ever". Everything else,
// including every transport error, is retryable.
func (transport *Transport) Deliver(ctx context.Context, message domain.Message) error {
	encoded, err := json.Marshal(payload{
		Version:     payloadVersion,
		EventID:     message.EventID,
		Type:        message.Type,
		ResourceRID: message.ResourceRID,
		Environment: message.Environment,
		Recipient:   message.Recipient,
		Subject:     message.Subject,
		Text:        message.Subject + "\n" + message.Body,
		Body:        message.Body,
		OccurredAt:  message.OccurredAt.UTC(),
	})
	if err != nil {
		// Nothing about a second attempt changes how this encodes.
		return fmt.Errorf("%w: encode notification webhook payload: %v", ports.ErrPermanentDelivery, err)
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, transport.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("%w: build notification webhook request: %v", ports.ErrPermanentDelivery, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := transport.client.Do(request)
	if err != nil {
		return fmt.Errorf("post notification webhook: %w", err)
	}
	defer response.Body.Close()
	// Drain a bounded prefix either way: on success so the connection can be
	// reused, and on failure so the reason an operator reads is the endpoint's
	// own words rather than a status code on its own.
	detail, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	if permanentStatus(response.StatusCode) {
		return fmt.Errorf("%w: notification webhook answered %d: %s",
			ports.ErrPermanentDelivery, response.StatusCode, summarize(detail))
	}
	return fmt.Errorf("notification webhook answered %d: %s",
		response.StatusCode, summarize(detail))
}

func permanentStatus(status int) bool {
	if status == http.StatusRequestTimeout || status == http.StatusTooManyRequests {
		return false
	}
	return status >= 400 && status < 500
}

// summarize keeps an endpoint that answers with a whole HTML page from filling
// the reason column, and collapses newlines so one failure stays one log line.
func summarize(detail []byte) string {
	text := strings.Join(strings.Fields(string(detail)), " ")
	const maximum = 200
	if len(text) > maximum {
		return text[:maximum]
	}
	if text == "" {
		return "(no body)"
	}
	return text
}
