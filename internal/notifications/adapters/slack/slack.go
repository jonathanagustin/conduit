// Package slack delivers Conduit notifications through Slack's public
// chat.postMessage API. It does not claim Apollo's private transport format.
// Contract: slackapi/slack-api-specs@bc08db49625630e3585bf2f1322128ea04f2a7f3,
// web-api/slack_web_openapi_v2_without_examples.json, /chat.postMessage.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

const DefaultTimeout = 10 * time.Second
const endpoint = "https://slack.com/api/chat.postMessage"

type Transport struct {
	token  string
	client *http.Client
}

var _ ports.Transport = (*Transport)(nil)

// New configures one Slack bot without logging or persisting its token.
func New(token string, client *http.Client) (*Transport, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("a Slack bot token is required")
	}
	for _, character := range token {
		if character <= ' ' || character >= 127 {
			return nil, errors.New("Slack bot token contains invalid characters")
		}
	}
	configured := http.Client{Timeout: DefaultTimeout}
	if client != nil {
		configured = *client
		if configured.Timeout <= 0 {
			configured.Timeout = DefaultTimeout
		}
	}
	// A redirect must never carry the credential to a different endpoint.
	configured.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Transport{token: token, client: &configured}, nil
}

func (*Transport) Name() string   { return "slack" }
func (*Transport) String() string { return "slack notification transport" }

func (transport *Transport) Deliver(ctx context.Context, message domain.Message) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if message.Recipient.Channel != domain.ChannelSlack || message.Destination.Kind != domain.ContactSlack || !message.Destination.Named() {
		return fmt.Errorf("%w: Slack delivery requires a Slack contact", ports.ErrPermanentDelivery)
	}
	payload := struct {
		Channel     string `json:"channel"`
		Text        string `json:"text"`
		Markdown    bool   `json:"mrkdwn"`
		Parse       string `json:"parse"`
		UnfurlLinks bool   `json:"unfurl_links"`
		UnfurlMedia bool   `json:"unfurl_media"`
	}{Channel: message.Destination.Address, Text: escapeText(message.Subject + "\n\n" + message.Body), Parse: "none"}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("encode Slack notification")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("construct Slack notification request")
	}
	request.Header.Set("Authorization", "Bearer "+transport.token)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := transport.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Slack notification request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		seconds, _ := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64)
		if seconds > 0 && seconds <= int64((1<<63-1)/int64(time.Second)) {
			return rateLimitError{delay: time.Duration(seconds) * time.Second}
		}
		return errors.New("Slack delivery rate limited")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode >= 500 {
			return fmt.Errorf("Slack delivery HTTP %d", response.StatusCode)
		}
		return fmt.Errorf("%w: Slack delivery HTTP %d", ports.ErrPermanentDelivery, response.StatusCode)
	}
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return errors.New("invalid Slack delivery response")
	}
	if result.OK {
		return nil
	}
	switch result.Error {
	case "not_authed", "invalid_auth", "account_inactive", "channel_not_found", "not_in_channel", "is_archived", "msg_too_long", "no_text", "too_many_attachments", "invalid_arg_name", "invalid_array_arg", "invalid_charset", "invalid_form_data", "invalid_post_type", "missing_post_type":
		return fmt.Errorf("%w: Slack delivery rejected (%s)", ports.ErrPermanentDelivery, result.Error)
	case "rate_limited":
		return errors.New("Slack delivery rate limited")
	default:
		return errors.New("Slack delivery rejected")
	}
}

type rateLimitError struct{ delay time.Duration }

func (rateLimitError) Error() string                     { return "Slack delivery rate limited" }
func (failure rateLimitError) RetryAfter() time.Duration { return failure.delay }

func escapeText(text string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(text)
}
