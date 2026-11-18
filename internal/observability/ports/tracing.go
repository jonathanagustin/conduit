// Package ports defines bounded tracing operations and context propagation.
package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

type Operation string

const (
	HubRequest     Operation = "hub.request"
	AgentLease     Operation = "agent.lease"
	AgentExecution Operation = "agent.execution"
	AgentApply     Operation = "agent.apply"
	PlanDecision   Operation = "plan.decision"
)

// Tracer accepts bounded operations, never arbitrary events or attributes.
type Tracer interface {
	Start(context.Context, Operation, domain.TraceContext) (context.Context, Span)
}

type Span interface {
	HTTP(route, method string, status int)
	Link(domain.TraceContext)
	End(failed bool)
}

// EndOnReturn preserves panics while recording their failed outcome.
// Call it directly with defer and the operation's named error result.
func EndOnReturn(span Span, result *error) {
	if value := recover(); value != nil {
		span.End(true)
		panic(value)
	}
	span.End(*result != nil)
}

type spanKey struct{}
type tracerKey struct{}

func ContextWithTracer(ctx context.Context, tracer Tracer) context.Context {
	return context.WithValue(ctx, tracerKey{}, tracer)
}
func TracerFromContext(ctx context.Context) (Tracer, bool) {
	tracer, ok := ctx.Value(tracerKey{}).(Tracer)
	return tracer, ok
}

func ContextWithSpan(ctx context.Context, span Span) context.Context {
	return context.WithValue(ctx, spanKey{}, span)
}

// RecordedTrace distinguishes opt-in tracing from default log correlation.
func RecordedTrace(ctx context.Context) (domain.TraceContext, bool) {
	if _, ok := ctx.Value(spanKey{}).(Span); !ok {
		return domain.TraceContext{}, false
	}
	return domain.TraceFromContext(ctx)
}

func Link(ctx context.Context, parent domain.TraceContext) {
	if span, ok := ctx.Value(spanKey{}).(Span); ok && parent.Valid() {
		span.Link(parent)
	}
}

// OutboundTraceparent propagates an exported span's actual position, not a fabricated child.
// Default log-only correlation retains its existing behavior.
func OutboundTraceparent(ctx context.Context) string {
	if current, ok := RecordedTrace(ctx); ok {
		return current.Traceparent()
	}
	return domain.OutboundTraceparent(ctx)
}
