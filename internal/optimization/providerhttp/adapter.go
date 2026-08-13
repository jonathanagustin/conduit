// Package providerhttp implements the bounded HTTP outbound adapter for
// optimizer acquisition reads. The optimization domain owns relative read
// intents; this adapter owns transport mechanics but never credentials.
package providerhttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

const MaximumResponseBytes = 1 << 20

// Route is an exact read capability, not a URL prefix. Route discovery and
// tenant scope happen outside the adapter; an unresolved or unlisted request
// never reaches the transport.
type Route struct {
	Source      string
	Endpoint    string
	Consistency string
}

// Adapter uses RoundTrip directly so redirects, cookie jars, and implicit
// client policy cannot widen a read. Workload identity, mTLS, or token exchange
// remains externally owned by the injected transport and is never represented
// in this value or its receipts.
type Adapter struct {
	base                 *url.URL
	transport            http.RoundTripper
	routes               map[Route]struct{}
	now                  func() time.Time
	maximumResponseBytes int
}

type Options struct {
	BaseURL              string
	Transport            http.RoundTripper
	Routes               []Route
	Now                  func() time.Time
	MaximumResponseBytes int
	// AllowLoopbackHTTP exists only for local/sanitized fixture servers. It
	// never permits a non-loopback plaintext provider.
	AllowLoopbackHTTP bool
}

func New(options Options) (*Adapter, error) {
	if options.Transport == nil || options.Now == nil {
		return nil, errors.New("provider HTTP adapter requires transport and clock")
	}
	base, err := url.Parse(options.BaseURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("provider HTTP adapter requires an absolute credential-free base URL")
	}
	hostname := base.Hostname()
	loopback := hostname == "localhost"
	if address := net.ParseIP(hostname); address != nil {
		loopback = address.IsLoopback()
	}
	if base.Scheme != "https" && !(options.AllowLoopbackHTTP && base.Scheme == "http" && loopback) {
		return nil, errors.New("provider HTTP adapter requires HTTPS outside loopback fixtures")
	}
	if base.Path != "" && base.Path != "/" {
		return nil, errors.New("provider HTTP adapter base URL cannot contain a path")
	}
	maximum := options.MaximumResponseBytes
	if maximum == 0 {
		maximum = MaximumResponseBytes
	}
	if maximum < 1 || maximum > MaximumResponseBytes {
		return nil, fmt.Errorf("provider HTTP response bound must be between 1 and %d bytes", MaximumResponseBytes)
	}
	routes := make(map[Route]struct{}, len(options.Routes))
	for _, route := range options.Routes {
		if err := validateRoute(route); err != nil {
			return nil, err
		}
		if _, exists := routes[route]; exists {
			return nil, errors.New("provider HTTP adapter routes must be unique")
		}
		routes[route] = struct{}{}
	}
	if len(routes) == 0 {
		return nil, errors.New("provider HTTP adapter requires at least one exact route")
	}
	canonicalBase := *base
	canonicalBase.Path = ""
	return &Adapter{base: &canonicalBase, transport: options.Transport, routes: routes, now: options.Now, maximumResponseBytes: maximum}, nil
}

func (adapter *Adapter) Read(ctx context.Context, request optimization.AcquisitionReadRequest) (optimization.AcquisitionReadResponse, error) {
	if adapter == nil || adapter.base == nil || adapter.transport == nil || adapter.now == nil {
		return optimization.AcquisitionReadResponse{}, optimization.AcquisitionReadError{Category: optimization.AcquisitionFailureProvider}
	}
	if ctx == nil {
		return optimization.AcquisitionReadResponse{}, errors.New("provider HTTP read requires a context")
	}
	if request.Method != http.MethodGet {
		return optimization.AcquisitionReadResponse{}, errors.New("provider HTTP adapter rejects non-GET requests")
	}
	route := Route{Source: request.Source, Endpoint: request.Endpoint, Consistency: request.Consistency}
	if _, allowed := adapter.routes[route]; !allowed {
		return optimization.AcquisitionReadResponse{}, optimization.AcquisitionReadError{Category: optimization.AcquisitionFailureAuthorization}
	}
	reference, err := parseRelativeEndpoint(request.Endpoint)
	if err != nil {
		return optimization.AcquisitionReadResponse{}, err
	}
	target := adapter.base.ResolveReference(reference)
	read, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return optimization.AcquisitionReadResponse{}, err
	}
	read.Header.Set("Accept", "application/json")
	response, err := adapter.transport.RoundTrip(read)
	if err != nil {
		if ctx.Err() != nil {
			return optimization.AcquisitionReadResponse{}, ctx.Err()
		}
		return optimization.AcquisitionReadResponse{}, optimization.AcquisitionReadError{Category: optimization.AcquisitionFailureTransport, Retryable: true}
	}
	if response == nil || response.Body == nil {
		return optimization.AcquisitionReadResponse{}, optimization.AcquisitionReadError{Category: optimization.AcquisitionFailureProvider}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(adapter.maximumResponseBytes)+1))
	if err != nil {
		return optimization.AcquisitionReadResponse{}, optimization.AcquisitionReadError{Category: optimization.AcquisitionFailureTransport, Retryable: true}
	}
	if len(body) == 0 || len(body) > adapter.maximumResponseBytes {
		return optimization.AcquisitionReadResponse{}, errors.New("provider HTTP response violates its exact byte bound")
	}
	observedAt := adapter.now().UTC()
	if observedAt.IsZero() {
		return optimization.AcquisitionReadResponse{}, errors.New("provider HTTP adapter clock returned zero time")
	}
	revision := boundedRevision("resource-version", response.Header.Get("X-Resource-Version"))
	if revision == "" {
		revision = boundedRevision("etag", response.Header.Get("ETag"))
	}
	if revision == "" {
		digest := sha256.Sum256(body)
		revision = "sha256:" + hex.EncodeToString(digest[:])
	}
	return optimization.AcquisitionReadResponse{
		StatusCode: response.StatusCode, ObservedAt: observedAt.Format(time.RFC3339Nano),
		SourceRevision: revision, Body: body,
	}, nil
}

func validateRoute(route Route) error {
	if strings.TrimSpace(route.Source) == "" || len(route.Source) > 256 || strings.TrimSpace(route.Consistency) == "" || len(route.Consistency) > 256 {
		return errors.New("provider HTTP route requires bounded source and consistency")
	}
	_, err := parseRelativeEndpoint(route.Endpoint)
	return err
}

func parseRelativeEndpoint(endpoint string) (*url.URL, error) {
	if endpoint == "" || len(endpoint) > 4096 || !strings.HasPrefix(endpoint, "/") || strings.Contains(endpoint, "\\") {
		return nil, errors.New("provider HTTP route requires a bounded absolute path")
	}
	reference, err := url.ParseRequestURI(endpoint)
	if err != nil || reference == nil {
		return nil, errors.New("provider HTTP route is not a canonical relative endpoint")
	}
	rawPath := strings.ToLower(reference.EscapedPath())
	if reference.IsAbs() || reference.Host != "" || reference.User != nil || reference.Fragment != "" || path.Clean(reference.Path) != reference.Path || strings.Contains(reference.Path, "\\") || strings.Contains(rawPath, "%2f") || strings.Contains(rawPath, "%5c") {
		return nil, errors.New("provider HTTP route is not a canonical relative endpoint")
	}
	return reference, nil
}

func boundedRevision(kind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return kind + "-sha256:" + hex.EncodeToString(digest[:])
}

var _ optimization.AcquisitionReadPort = (*Adapter)(nil)
