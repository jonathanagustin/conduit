package composition

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	obs "github.com/jonathanagustin/conduit/internal/observability/domain"
	"github.com/jonathanagustin/conduit/internal/observability/ports"
)

type requestTestTracer struct {
	parent obs.TraceContext
	span   requestTestSpan
}
type requestTestSpan struct {
	route, method string
	status        int
	ended, failed bool
}

func (tracer *requestTestTracer) Start(ctx context.Context, operation ports.Operation, parent obs.TraceContext) (context.Context, ports.Span) {
	tracer.parent = parent
	current := parent
	if current.Valid() {
		current = current.Child()
	} else {
		current = obs.NewTraceContext()
	}
	return ports.ContextWithSpan(obs.ContextWithTrace(ctx, current), &tracer.span), &tracer.span
}
func (span *requestTestSpan) HTTP(route, method string, status int) {
	span.route, span.method, span.status = route, method, status
}
func (*requestTestSpan) Link(obs.TraceContext) {}
func (span *requestTestSpan) End(failed bool)  { span.ended, span.failed = true, failed }

func TestObservedRequestContinuesParentAndPreservesPanicSemantics(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "panic"}[panics], func(t *testing.T) {
			parent := obs.NewTraceContext()
			tracer := &requestTestTracer{}
			handler := ObserveRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				current, ok := ports.RecordedTrace(r.Context())
				if !ok || current.TraceID != parent.TraceID || current.SpanID == parent.SpanID {
					t.Fatal("handler lost server context")
				}
				if panics {
					panic("preserved panic")
				}
				w.WriteHeader(http.StatusNoContent)
			}), controlplane.HandlerOptions{}, RequestObservation{Tracer: tracer})
			request := httptest.NewRequest(http.MethodGet, "/v1/plans/private-fixture-id", nil)
			request.Header.Set("traceparent", parent.Traceparent())
			var recovered any
			func() { defer func() { recovered = recover() }(); handler.ServeHTTP(httptest.NewRecorder(), request) }()
			if panics && recovered != "preserved panic" || !panics && recovered != nil {
				t.Fatal("tracing changed panic behavior")
			}
			if tracer.parent != parent || tracer.span.route != "/v1/plans/{planID}" || tracer.span.method != http.MethodGet || !tracer.span.ended || tracer.span.failed != panics {
				t.Fatalf("incorrect observation: %#v", tracer)
			}
			expected := http.StatusNoContent
			if panics {
				expected = http.StatusInternalServerError
			}
			if tracer.span.status != expected {
				t.Fatal("incorrect traced status")
			}
		})
	}
}
