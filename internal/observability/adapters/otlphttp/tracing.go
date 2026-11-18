// Package otlphttp provides opt-in, bounded export of tenant-free trace spans.
package otlphttp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
	"github.com/jonathanagustin/conduit/internal/observability/ports"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const EndpointEnvironment = "CONDUIT_OTLP_HTTP_ENDPOINT"
const SampleRatioEnvironment = "CONDUIT_TRACE_SAMPLE_RATIO"
const CAFileEnvironment = "CONDUIT_OTLP_CA_FILE"

type Recorder struct {
	provider *sdktrace.TracerProvider
	routes   map[string]bool
}

// New is inert without the explicit endpoint. Standard OTEL_* environment
// variables never enable export or add headers, resources or authentication.
func New(ctx context.Context, getenv func(string) string, service string, routes []string) (*Recorder, error) {
	endpoint := strings.TrimSpace(getenv(EndpointEnvironment))
	if endpoint == "" {
		return nil, nil
	}
	if service != "conduit-hub" && service != "conduit-agent" {
		return nil, fmt.Errorf("unknown tracing service")
	}
	u, err := url.Parse(endpoint)
	if len(endpoint) > 2048 || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("tracing endpoint must be HTTPS without credentials, query or fragment")
	}
	ratio := 0.01
	if raw := strings.TrimSpace(getenv(SampleRatioEnvironment)); raw != "" {
		ratio, err = strconv.ParseFloat(raw, 64)
		if err != nil || !(ratio >= 0 && ratio <= 1) {
			return nil, fmt.Errorf("trace sample ratio must be between zero and one")
		}
	}
	// A private client prevents environment-derived exporter credentials,
	// redirects or proxy credentials from changing the admitted destination.
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if path := strings.TrimSpace(getenv(CAFileEnvironment)); path != "" {
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read tracing CA file")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load tracing system roots")
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("tracing CA file contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	client := &http.Client{Timeout: 3 * time.Second,
		Transport:     &http.Transport{TLSClientConfig: tlsConfig},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint),
		otlptracehttp.WithHTTPClient(client), otlptracehttp.WithHeaders(map[string]string{}),
		otlptracehttp.WithTimeout(3*time.Second), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))
	if err != nil {
		return nil, fmt.Errorf("configure tracing exporter")
	}
	fixedResource := resource.NewWithAttributes("", attribute.String("service.name", service))
	allowed := map[string]bool{"unmatched": true}
	for _, route := range routes {
		allowed[route] = true
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithRawSpanLimits(sdktrace.SpanLimits{AttributeValueLengthLimit: 256, AttributeCountLimit: 8, LinkCountLimit: 4}),
		sdktrace.WithResource(fixedResource),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(ratio)),
		sdktrace.WithBatcher(&allowlistedExporter{inner: exporter, resource: fixedResource, routes: allowed}, sdktrace.WithMaxQueueSize(256), sdktrace.WithMaxExportBatchSize(64), sdktrace.WithBatchTimeout(5*time.Second), sdktrace.WithExportTimeout(3*time.Second)),
	)
	return &Recorder{provider: provider, routes: allowed}, nil
}

// The SDK merges ambient resources even with WithResource. Enforce the
// disclosure boundary on every exported span, not just at instrumentation.
type allowlistedExporter struct {
	inner    sdktrace.SpanExporter
	resource *resource.Resource
	routes   map[string]bool
}

func (exporter *allowlistedExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	clean := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, span := range spans {
		clean[i] = safeSpan{ReadOnlySpan: span, resource: exporter.resource, routes: exporter.routes}
	}
	if err := exporter.inner.ExportSpans(ctx, clean); err != nil {
		// The SDK can include a collector response body in its error. Do not
		// send that untrusted text to its process-wide error logger.
		return fmt.Errorf("trace export failed")
	}
	return nil
}
func (exporter *allowlistedExporter) Shutdown(ctx context.Context) error {
	return exporter.inner.Shutdown(ctx)
}

type safeSpan struct {
	sdktrace.ReadOnlySpan
	resource *resource.Resource
	routes   map[string]bool
}

func (span safeSpan) Resource() *resource.Resource { return span.resource }
func (span safeSpan) InstrumentationScope() instrumentation.Scope {
	return instrumentation.Scope{Name: "conduit"}
}
func (span safeSpan) InstrumentationLibrary() instrumentation.Scope {
	return span.InstrumentationScope()
}
func (span safeSpan) Events() []sdktrace.Event { return nil }
func (span safeSpan) SpanContext() trace.SpanContext {
	return span.ReadOnlySpan.SpanContext().WithTraceState(trace.TraceState{})
}
func (span safeSpan) Parent() trace.SpanContext {
	return span.ReadOnlySpan.Parent().WithTraceState(trace.TraceState{})
}
func (span safeSpan) Status() sdktrace.Status {
	return sdktrace.Status{Code: span.ReadOnlySpan.Status().Code}
}
func (span safeSpan) Name() string {
	switch name := span.ReadOnlySpan.Name(); ports.Operation(name) {
	case ports.HubRequest, ports.AgentLease, ports.AgentExecution, ports.AgentApply, ports.PlanDecision:
		return name
	}
	return "operation"
}
func (span safeSpan) Links() []sdktrace.Link {
	links := span.ReadOnlySpan.Links()
	out := make([]sdktrace.Link, 0, len(links))
	for _, link := range links {
		out = append(out, sdktrace.Link{SpanContext: link.SpanContext.WithTraceState(trace.TraceState{})})
	}
	return out
}
func (span safeSpan) Attributes() []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, 3)
	for _, attr := range span.ReadOnlySpan.Attributes() {
		switch string(attr.Key) {
		case "http.route":
			if attr.Value.Type() == attribute.STRING && span.routes[attr.Value.AsString()] {
				out = append(out, attr)
			}
		case "http.request.method":
			if attr.Value.Type() == attribute.STRING {
				switch attr.Value.AsString() {
				case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "OTHER":
					out = append(out, attr)
				}
			}
		case "http.response.status_code":
			if attr.Value.Type() == attribute.INT64 && attr.Value.AsInt64() >= 100 && attr.Value.AsInt64() <= 599 {
				out = append(out, attr)
			}
		}
	}
	return out
}

// Shutdown flushes within a bounded deadline independent of signal cancellation.
func (recorder *Recorder) Shutdown() error {
	if recorder == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return recorder.provider.Shutdown(ctx)
}

func spanContext(parent domain.TraceContext) trace.SpanContext {
	tid, _ := trace.TraceIDFromHex(parent.TraceID)
	sid, _ := trace.SpanIDFromHex(parent.SpanID)
	var flags trace.TraceFlags
	if parent.Sampled {
		flags = trace.FlagsSampled
	}
	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: flags, Remote: true})
}

func (recorder *Recorder) Start(ctx context.Context, operation ports.Operation, parent domain.TraceContext) (context.Context, ports.Span) {
	name := string(operation)
	kind := trace.SpanKindInternal
	switch operation {
	case ports.HubRequest:
		kind = trace.SpanKindServer
	case ports.AgentLease:
		kind = trace.SpanKindClient
	case ports.AgentExecution, ports.AgentApply, ports.PlanDecision:
	default:
		name = "operation"
	}
	if parent.Valid() {
		ctx = trace.ContextWithRemoteSpanContext(ctx, spanContext(parent))
	}
	ctx, raw := recorder.provider.Tracer("conduit").Start(ctx, name, trace.WithSpanKind(kind))
	sc := raw.SpanContext()
	ctx = domain.ContextWithTrace(ctx, domain.TraceContext{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String(), Sampled: sc.IsSampled()})
	span := &recordedSpan{raw: raw, routes: recorder.routes}
	return ports.ContextWithTracer(ports.ContextWithSpan(ctx, span), recorder), span
}

type recordedSpan struct {
	raw    trace.Span
	routes map[string]bool
}

func (span *recordedSpan) HTTP(route, method string, status int) {
	if !span.routes[route] {
		route = "unmatched"
	}
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
	default:
		method = "OTHER"
	}
	if status < 100 || status > 599 {
		status = 0
	}
	span.raw.SetAttributes(attribute.String("http.route", route), attribute.String("http.request.method", method), attribute.Int("http.response.status_code", status))
}

func (span *recordedSpan) Link(parent domain.TraceContext) {
	if parent.Valid() {
		span.raw.AddLink(trace.Link{SpanContext: spanContext(parent)})
	}
}

func (span *recordedSpan) End(failed bool) {
	if failed {
		span.raw.SetStatus(codes.Error, "")
	}
	span.raw.End()
}
