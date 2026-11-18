package controlplane

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	observability "github.com/jonathanagustin/conduit/internal/observability/domain"
	traceports "github.com/jonathanagustin/conduit/internal/observability/ports"
)

type decisionTestTracer struct {
	traces []observability.TraceContext
	links  []observability.TraceContext
}
type decisionTestSpan struct{ tracer *decisionTestTracer }

func (tracer *decisionTestTracer) Start(ctx context.Context, _ traceports.Operation, parent observability.TraceContext) (context.Context, traceports.Span) {
	if !parent.Valid() {
		parent, _ = observability.TraceFromContext(ctx)
	}
	if parent.Valid() {
		parent = parent.Child()
	} else {
		parent = observability.NewTraceContext()
	}
	tracer.traces = append(tracer.traces, parent)
	span := &decisionTestSpan{tracer: tracer}
	ctx = observability.ContextWithTrace(ctx, parent)
	return traceports.ContextWithTracer(traceports.ContextWithSpan(ctx, span), tracer), span
}
func (*decisionTestSpan) HTTP(string, string, int) {}
func (*decisionTestSpan) End(bool)                 {}
func (span *decisionTestSpan) Link(parent observability.TraceContext) {
	span.tracer.links = append(span.tracer.links, parent)
}

func TestDecisionTraceSurvivesReopenWithoutChangingPlanIdentityOrIdempotence(t *testing.T) {
	ctx := context.Background()
	plain, err := NewLifecycle(NewMemoryRepository()).CreatePlan(ctx, foundation())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "repository")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	tracer := &decisionTestTracer{}
	traced := traceports.ContextWithTracer(ctx, tracer)
	first, err := NewLifecycle(repository).CreatePlan(traced, foundation())
	if err != nil {
		t.Fatal(err)
	}
	parent, ok := observability.ParseTraceparent(first.DecisionTraceparent)
	if !ok || len(tracer.traces) != 1 || parent != tracer.traces[0] {
		t.Fatal("accepted decision context not retained")
	}
	if first.ID != plain.ID || first.DesiredStateDigest != plain.DesiredStateDigest || !reflect.DeepEqual(first.Plan, plain.Plan) {
		t.Fatal("tracing changed Plan identity or content")
	}
	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(reopened)
	again, err := lifecycle.CreatePlan(traced, foundation())
	if err != nil {
		t.Fatal(err)
	}
	if len(tracer.traces) != 2 || tracer.traces[1] == parent || again.DecisionTraceparent != first.DecisionTraceparent {
		t.Fatal("retry replaced original decision context")
	}
	if _, err = lifecycle.ApprovePlan(ctx, first.ID, "reviewer", "test decision"); err != nil {
		t.Fatal(err)
	}
	leaseCtx, _ := tracer.Start(ctx, traceports.HubRequest, observability.TraceContext{})
	leased, err := lifecycle.LeaseNextPlan(leaseCtx, first.Plan.Environment, "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if leased.DecisionTraceparent != first.DecisionTraceparent || len(tracer.links) != 1 || tracer.links[0] != parent {
		t.Fatal("lease did not link original decision")
	}
}

func TestLogCorrelationAloneDoesNotPersistDecisionTrace(t *testing.T) {
	ctx := observability.ContextWithTrace(context.Background(), observability.NewTraceContext())
	record, err := NewLifecycle(NewMemoryRepository()).CreatePlan(ctx, foundation())
	if err != nil {
		t.Fatal(err)
	}
	if record.DecisionTraceparent != "" {
		t.Fatal("default log correlation enabled persisted tracing")
	}
}
