package providerhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestAdapterExecutesOnlyExactCredentialFreeBoundedReads(t *testing.T) {
	observed := time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC)
	invocations := 0
	adapter, err := New(Options{
		BaseURL: "https://provider.example.test", Now: func() time.Time { return observed },
		Routes: []Route{{Source: "kubernetes", Endpoint: "/api/v1/namespaces/tenant-a/pods?limit=500", Consistency: "resource-version"}},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			invocations++
			if request.Method != http.MethodGet || request.URL.String() != "https://provider.example.test/api/v1/namespaces/tenant-a/pods?limit=500" || request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
				t.Fatalf("transport request = %#v headers=%#v", request, request.Header)
			}
			return &http.Response{
				StatusCode: http.StatusOK, Header: http.Header{"X-Resource-Version": []string{"rv-17"}},
				Body: io.NopCloser(strings.NewReader(`{"metadata":{"resourceVersion":"rv-17"},"items":[]}`)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := adapter.Read(context.Background(), optimization.AcquisitionReadRequest{
		Source: "kubernetes", Method: http.MethodGet, Endpoint: "/api/v1/namespaces/tenant-a/pods?limit=500", Consistency: "resource-version",
	})
	if err != nil {
		t.Fatal(err)
	}
	if invocations != 1 || response.StatusCode != http.StatusOK || response.SourceRevision != "resource-version-sha256:cf33fd333fa715d93cb89c7275c79185b1ff4fd5395d379bcd1e43344367dcb1" || response.ObservedAt != observed.Format(time.RFC3339Nano) || len(response.Body) == 0 {
		t.Fatalf("response = %#v invocations=%d", response, invocations)
	}
}

func TestAdapterFailsClosedBeforeTransportAndBoundsProviderOutput(t *testing.T) {
	invocations := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		invocations++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("12345"))}, nil
	})
	adapter, err := New(Options{
		BaseURL: "http://127.0.0.1:8080", AllowLoopbackHTTP: true, Transport: transport,
		Routes: []Route{{Source: "opencost", Endpoint: "/model/allocation?window=1h", Consistency: "windowed"}},
		Now:    func() time.Time { return time.Date(2026, time.August, 12, 12, 0, 0, 0, time.UTC) }, MaximumResponseBytes: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	requests := []optimization.AcquisitionReadRequest{
		{Source: "opencost", Method: http.MethodPost, Endpoint: "/model/allocation?window=1h", Consistency: "windowed"},
		{Source: "opencost", Method: http.MethodGet, Endpoint: "/model/allocation?window=2h", Consistency: "windowed"},
		{Source: "opencost", Method: http.MethodGet, Endpoint: "https://attacker.test/state", Consistency: "windowed"},
	}
	for _, request := range requests {
		if _, err := adapter.Read(context.Background(), request); err == nil {
			t.Fatalf("accepted unsafe request %#v", request)
		}
	}
	if invocations != 0 {
		t.Fatalf("unsafe requests reached transport %d times", invocations)
	}
	_, err = adapter.Read(context.Background(), optimization.AcquisitionReadRequest{Source: "opencost", Method: http.MethodGet, Endpoint: "/model/allocation?window=1h", Consistency: "windowed"})
	if err == nil || !strings.Contains(err.Error(), "byte bound") || invocations != 1 {
		t.Fatalf("oversized response err=%v invocations=%d", err, invocations)
	}

	if _, err := New(Options{BaseURL: "http://provider.example.test", Transport: transport, Routes: []Route{{Source: "x", Endpoint: "/x", Consistency: "x"}}, Now: time.Now}); err == nil {
		t.Fatal("accepted plaintext non-loopback provider")
	}
	if _, err := New(Options{BaseURL: "https://user:secret@provider.example.test", Transport: transport, Routes: []Route{{Source: "x", Endpoint: "/x", Consistency: "x"}}, Now: time.Now}); err == nil {
		t.Fatal("accepted credentials in provider base URL")
	}
	for _, endpoint := range []string{"/%", "/safe/%2f/foreign", "/safe/%5c/foreign", "/safe/%2e%2e/foreign"} {
		if _, err := New(Options{BaseURL: "https://provider.example.test", Transport: transport, Routes: []Route{{Source: "x", Endpoint: endpoint, Consistency: "x"}}, Now: time.Now}); err == nil {
			t.Fatalf("accepted non-canonical encoded endpoint %q", endpoint)
		}
	}
}

func TestAdapterReducesTransportErrorsToBoundedCategory(t *testing.T) {
	adapter, err := New(Options{
		BaseURL: "https://provider.example.test", Routes: []Route{{Source: "prometheus", Endpoint: "/api/v1/query?query=up", Consistency: "instant"}}, Now: time.Now,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("credential-bearing provider detail")
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.Read(context.Background(), optimization.AcquisitionReadRequest{Source: "prometheus", Method: http.MethodGet, Endpoint: "/api/v1/query?query=up", Consistency: "instant"})
	var readError optimization.AcquisitionReadError
	if !errors.As(err, &readError) || readError.Category != optimization.AcquisitionFailureTransport || !readError.Retryable || strings.Contains(err.Error(), "credential-bearing") {
		t.Fatalf("transport error = %#v", err)
	}
}
