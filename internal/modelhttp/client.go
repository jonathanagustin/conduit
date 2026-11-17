// Package modelhttp calls a model served over HTTP through the
// OpenAI-compatible Chat Completions API. It is the shared transport behind
// the per-context model adapters, the way localcmd is the shared transport
// behind the modelcli adapters. It does not approve a change.
//
// One request, one response: POST {base}/chat/completions with stream=false.
// The ports this serves are request/response, so streaming is not implemented.
// The Anthropic Messages API is not implemented.
package modelhttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// AdapterName is what a port adapter records as the provenance adapter.
const AdapterName = "openai-http"

const (
	// DefaultTimeout matches the local model CLI default.
	DefaultTimeout = 45 * time.Second
	// DefaultMaxResponseBytes bounds one completion body.
	DefaultMaxResponseBytes int64 = 4 << 20
	maxKeyFileBytes               = 16 << 10
	redirectLimit                 = 10
)

// The error values a caller can tell apart with errors.Is. None of them
// carries the key, the prompt, or the response body.
var (
	ErrNotConfigured    = errors.New("model HTTP endpoint is not configured")
	ErrInsecureEndpoint = errors.New("model HTTP endpoint must use https")
	ErrInvalidConfig    = errors.New("model HTTP configuration is not valid")
	ErrKeyFile          = errors.New("model API key file is not usable")
	ErrTimeout          = errors.New("model HTTP request timed out")
	ErrCanceled         = errors.New("model HTTP request was cancelled")
	ErrTransport        = errors.New("model HTTP request failed")
	ErrRedirectRefused  = errors.New("model HTTP endpoint redirected to another origin")
	ErrUnauthorized     = errors.New("model HTTP endpoint refused the credential")
	ErrRateLimited      = errors.New("model HTTP endpoint is rate limiting")
	ErrUpstream         = errors.New("model HTTP endpoint failed")
	ErrStatus           = errors.New("model HTTP endpoint returned an unexpected status")
	ErrResponseTooLarge = errors.New("model HTTP response is too large")
	ErrMalformed        = errors.New("model HTTP response is not a chat completion")
)

// RateLimitError is a 429. RetryAfter is zero when the server sent no usable
// Retry-After header. errors.Is(err, ErrRateLimited) matches it.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (err *RateLimitError) Error() string {
	if err.RetryAfter > 0 {
		return fmt.Sprintf("%v: retry after %s", ErrRateLimited, err.RetryAfter)
	}
	return ErrRateLimited.Error() + ": no Retry-After"
}

func (err *RateLimitError) Is(target error) bool { return target == ErrRateLimited }

// Config is the whole configuration. The key is named by file, never by
// value, and is read on every call so a rotated key needs no restart.
type Config struct {
	BaseURL           string
	Model             string
	APIKeyFile        string
	CAFile            string
	AllowInsecureHTTP bool
	Timeout           time.Duration
	MaxResponseBytes  int64
}

// Completion is one reply. Model is the id the server reported in the
// response, which can differ from the id requested.
type Completion struct {
	Text  string
	Model string
}

// Client is safe for concurrent use.
type Client struct {
	endpoint string
	model    string
	keyFile  string
	timeout  time.Duration
	maxBytes int64
	http     *http.Client
	reported atomic.Value
}

// New validates the configuration and builds the client. It makes no call.
func New(config Config) (*Client, error) {
	endpoint, err := endpointURL(config.BaseURL, config.AllowInsecureHTTP)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		return nil, fmt.Errorf("%w: a model name is required", ErrInvalidConfig)
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("%w: default transport is not usable", ErrInvalidConfig)
	}
	transport = transport.Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile := strings.TrimSpace(config.CAFile); caFile != "" {
		pool, err := rootsWith(caFile)
		if err != nil {
			return nil, err
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	keyFile := strings.TrimSpace(config.APIKeyFile)
	if keyFile != "" {
		if _, err := readKey(keyFile); err != nil {
			return nil, err
		}
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxBytes := config.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxResponseBytes
	}
	return &Client{
		endpoint: endpoint, model: model, keyFile: keyFile, timeout: timeout, maxBytes: maxBytes,
		http: &http.Client{Transport: transport, CheckRedirect: checkRedirect},
	}, nil
}

// RequestedModel is the configured model name.
func (client *Client) RequestedModel() string {
	if client == nil {
		return ""
	}
	return client.model
}

// ReportedModel is the model id from the most recent successful response, or
// empty before the first one. The model ports have no field for it.
func (client *Client) ReportedModel() string {
	if client == nil {
		return ""
	}
	reported, _ := client.reported.Load().(string)
	return reported
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Complete sends the prompt as one user message and returns the first choice.
func (client *Client) Complete(ctx context.Context, prompt string) (Completion, error) {
	if client == nil || client.http == nil || client.endpoint == "" {
		return Completion{}, ErrNotConfigured
	}
	body, err := json.Marshal(chatRequest{
		Model: client.model, Messages: []chatMessage{{Role: "user", Content: prompt}}, Stream: false,
	})
	if err != nil {
		return Completion{}, fmt.Errorf("%w: encode request", ErrTransport)
	}
	ctx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		return Completion{}, fmt.Errorf("%w: build request", ErrTransport)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if client.keyFile != "" {
		key, err := readKey(client.keyFile)
		if err != nil {
			return Completion{}, err
		}
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return Completion{}, transportError(ctx, err)
	}
	defer response.Body.Close()
	if err := statusError(response); err != nil {
		return Completion{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, client.maxBytes+1))
	if err != nil {
		return Completion{}, transportError(ctx, err)
	}
	if int64(len(raw)) > client.maxBytes {
		return Completion{}, fmt.Errorf("%w: more than %d bytes", ErrResponseTooLarge, client.maxBytes)
	}
	var decoded chatResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Completion{}, fmt.Errorf("%w: body is not JSON", ErrMalformed)
	}
	if len(decoded.Choices) == 0 || decoded.Choices[0].Message.Content == nil {
		return Completion{}, fmt.Errorf("%w: no message content", ErrMalformed)
	}
	reported := strings.TrimSpace(decoded.Model)
	if reported != "" {
		client.reported.Store(reported)
	}
	return Completion{Text: *decoded.Choices[0].Message.Content, Model: reported}, nil
}

// statusError classifies a non-200 status. The body is not read into the
// error: an error body can echo the prompt.
func statusError(response *http.Response) error {
	switch code := response.StatusCode; {
	case code == http.StatusOK:
		return nil
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, code)
	case code == http.StatusTooManyRequests:
		return &RateLimitError{RetryAfter: retryAfter(response.Header.Get("Retry-After"), time.Now())}
	case code >= 500:
		return fmt.Errorf("%w: status %d", ErrUpstream, code)
	default:
		return fmt.Errorf("%w: status %d", ErrStatus, code)
	}
}

// retryAfter reads delay-seconds or an HTTP date. Anything else is zero.
func retryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now).Round(time.Second)
	}
	return 0
}

// transportError drops the *url.Error text, which repeats the URL, and keeps
// only the class of failure.
func transportError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, ErrRedirectRefused):
		return ErrRedirectRefused
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return ErrTimeout
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return fmt.Errorf("%w: %w", ErrCanceled, context.Canceled)
	}
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &unknown) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return fmt.Errorf("%w: server certificate is not trusted", ErrTransport)
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return ErrTimeout
	}
	return fmt.Errorf("%w: connection error", ErrTransport)
}

// checkRedirect refuses any redirect that leaves the exact origin the request
// was addressed to. internal/gitless's registrychallenge.CheckRedirect strips
// the credential and follows, because registries redirect blob reads to a CDN.
// A model endpoint has no such need, and a 307 or 308 would re-send the prompt
// body as well as the header, so stripping the header alone would not be
// enough here. The Authorization header is deleted before refusing so that no
// later change to the return value can put it back on the wire.
func checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) >= redirectLimit {
		return fmt.Errorf("%w: stopped after %d redirects", ErrRedirectRefused, redirectLimit)
	}
	if sameOrigin(request.URL, via[0].URL) {
		return nil
	}
	request.Header.Del("Authorization")
	return ErrRedirectRefused
}

// sameOrigin compares scheme and the whole host, port included. A subdomain
// is another origin; net/http's own rule admits subdomains.
func sameOrigin(next, original *url.URL) bool {
	return strings.EqualFold(next.Scheme, original.Scheme) && strings.EqualFold(next.Host, original.Host)
}

// endpointURL validates the base URL and appends /chat/completions.
func endpointURL(base string, allowInsecure bool) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", ErrNotConfigured
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" {
		return "", fmt.Errorf("%w: base URL must be an absolute URL", ErrInvalidConfig)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("%w: base URL must not carry credentials", ErrInvalidConfig)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("%w: base URL must not carry a query or fragment", ErrInvalidConfig)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
	case "http":
		if !allowInsecure && !isLoopback(parsed.Hostname()) {
			return "", fmt.Errorf("%w: %s is not loopback", ErrInsecureEndpoint, parsed.Hostname())
		}
	default:
		return "", fmt.Errorf("%w: base URL scheme must be https", ErrInvalidConfig)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/chat/completions"
	parsed.RawPath = ""
	return parsed.String(), nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func rootsWith(caFile string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("%w: CA file cannot be read", ErrInvalidConfig)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%w: CA file holds no PEM certificate", ErrInvalidConfig)
	}
	return pool, nil
}

// readKey returns the trimmed file content. The error never holds the content.
func readKey(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%w: cannot be read", ErrKeyFile)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxKeyFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: cannot be read", ErrKeyFile)
	}
	if len(raw) > maxKeyFileBytes {
		return "", fmt.Errorf("%w: too large", ErrKeyFile)
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return "", fmt.Errorf("%w: empty", ErrKeyFile)
	}
	for _, char := range key {
		if char < 0x21 || char > 0x7e {
			return "", fmt.Errorf("%w: holds a character that is not allowed in a header", ErrKeyFile)
		}
	}
	return key, nil
}
