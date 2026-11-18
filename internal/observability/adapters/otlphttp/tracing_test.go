package otlphttp

import (
	"context"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
	"github.com/jonathanagustin/conduit/internal/observability/ports"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	collectorpb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func config(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestDisabledIgnoresAmbientOpenTelemetryConfiguration(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://unused.invalid/v1/traces")
	r, err := New(context.Background(), config(nil), "conduit-hub", nil)
	if err != nil || r != nil {
		t.Fatalf("disabled exporter = %v, %v", r, err)
	}
	if err := r.Shutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestConfigurationRefusesUnsafeDestinationsAndSampling(t *testing.T) {
	for _, endpoint := range []string{"http://collector/v1/traces", "https://user:password@collector/v1/traces", "https://collector/v1/traces?token=secret", "https://collector/v1/traces#fragment", "https:///v1/traces"} {
		if r, err := New(context.Background(), config(map[string]string{EndpointEnvironment: endpoint}), "conduit-hub", nil); err == nil || r != nil {
			t.Fatalf("accepted endpoint %q", endpoint)
		}
	}
	for _, ratio := range []string{"-1", "1.1", "NaN", "+Inf", "invalid"} {
		if r, err := New(context.Background(), config(map[string]string{EndpointEnvironment: "https://collector/v1/traces", SampleRatioEnvironment: ratio}), "conduit-agent", nil); err == nil || r != nil {
			t.Fatalf("accepted ratio %q", ratio)
		}
	}
}

func TestTLSCollectorReceivesRealParentageAndOnlyAllowedAttributes(t *testing.T) {
	var mu sync.Mutex
	var requests []*collectorpb.ExportTraceServiceRequest
	collector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "" || r.Header.Get("X-Untrusted") != "" {
			t.Errorf("unexpected exporter request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			t.Error(err)
			return
		}
		var request collectorpb.ExportTraceServiceRequest
		if err := proto.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		requests = append(requests, &request)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer collector.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: collector.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "Authorization=Bearer%20secret,X-Untrusted=secret")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "tenant=secret")
	r, err := New(context.Background(), config(map[string]string{EndpointEnvironment: collector.URL + "/v1/traces", CAFileEnvironment: ca, SampleRatioEnvironment: "1"}), "conduit-hub", []string{"/v1/plans/{planID}"})
	if err != nil {
		t.Fatal(err)
	}
	parent := domain.TraceContext{TraceID: "00112233445566778899aabbccddeeff", SpanID: "0011223344556677", Sampled: true}
	ctx, server := r.Start(context.Background(), ports.HubRequest, parent)
	current, ok := ports.RecordedTrace(ctx)
	if !ok || current.TraceID != parent.TraceID || current.SpanID == parent.SpanID {
		t.Fatal("server did not continue the admitted parent")
	}
	if ports.OutboundTraceparent(ctx) != current.Traceparent() {
		t.Fatal("outbound parent is not the exported span")
	}
	// Even instrumentation accidentally using the SDK directly cannot expand
	// the export boundary or attach raw errors and tenant-bearing attributes.
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("tenant", "secret"), attribute.String("url.full", "https://private-tenant.invalid/secret"))
	trace.SpanFromContext(ctx).SetStatus(codes.Error, "secret-error")
	_, apply := r.Start(ctx, ports.AgentApply, domain.TraceContext{})
	apply.End(true)
	server.HTTP("/v1/plans/private-tenant", "secret-method", 500)
	server.End(false)
	if err := r.Shutdown(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, request := range requests {
		for _, rs := range request.ResourceSpans {
			if len(rs.Resource.Attributes) != 1 || rs.Resource.Attributes[0].Key != "service.name" || rs.Resource.Attributes[0].Value.GetStringValue() != "conduit-hub" {
				t.Fatal("ambient resource attributes escaped")
			}
			for _, scope := range rs.ScopeSpans {
				for _, span := range scope.Spans {
					count++
					if string(span.TraceId) != string(mustHex(t, parent.TraceID)) {
						t.Fatal("exported trace identity changed")
					}
					if len(span.Events) != 0 {
						t.Fatal("unexpected span events")
					}
					if span.Status.GetMessage() != "" {
						t.Fatal("raw status message escaped")
					}
					switch span.Name {
					case "hub.request":
						if string(span.SpanId) != string(mustHex(t, current.SpanID)) {
							t.Fatal("server exported a different span position")
						}
						if string(span.ParentSpanId) != string(mustHex(t, parent.SpanID)) {
							t.Fatal("server parent lost")
						}
						if len(span.Attributes) != 3 {
							t.Fatal("unexpected attribute set")
						}
						for _, a := range span.Attributes {
							if a.Key == "http.route" && a.Value.GetStringValue() != "unmatched" {
								t.Fatal("raw path escaped")
							}
							if a.Key == "http.request.method" && a.Value.GetStringValue() != "OTHER" {
								t.Fatal("raw method escaped")
							}
						}
					case "agent.apply":
						if string(span.ParentSpanId) != string(mustHex(t, current.SpanID)) {
							t.Fatal("apply parent lost")
						}
					default:
						t.Fatalf("unbounded operation %q", span.Name)
					}
				}
			}
		}
	}
	if count != 2 {
		t.Fatalf("exported %d spans, want 2", count)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	out, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestConfigurationErrorsDoNotEchoEndpointCredentials(t *testing.T) {
	_, err := New(context.Background(), config(map[string]string{EndpointEnvironment: "https://user:secret@collector/v1/traces"}), "conduit-hub", nil)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("endpoint credential exposed")
	}
}

func TestZeroSamplingIgnoresInboundAndAmbientAlwaysOnFlags(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	collector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer collector.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: collector.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	r, err := New(context.Background(), config(map[string]string{EndpointEnvironment: collector.URL + "/v1/traces", CAFileEnvironment: ca, SampleRatioEnvironment: "0"}), "conduit-agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	parent := domain.NewTraceContext()
	parent.Sampled = true
	ctx, span := r.Start(context.Background(), ports.AgentExecution, parent)
	current, ok := ports.RecordedTrace(ctx)
	if !ok || current.TraceID != parent.TraceID || current.Sampled {
		t.Fatal("sampling policy was overridden by caller or environment")
	}
	span.End(false)
	if err := r.Shutdown(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 0 {
		t.Fatal("zero sampling exported spans")
	}
}

type failingCollector struct{}

func (failingCollector) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return errors.New("untrusted collector body: secret")
}
func (failingCollector) Shutdown(context.Context) error { return nil }

func TestCollectorErrorBodyDoesNotReachTheSDKErrorLogger(t *testing.T) {
	exporter := &allowlistedExporter{inner: failingCollector{}}
	err := exporter.ExportSpans(context.Background(), nil)
	if err == nil || err.Error() != "trace export failed" {
		t.Fatal("collector response escaped the exporter boundary")
	}
}
