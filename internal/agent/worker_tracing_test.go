package agent

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	obs "github.com/jonathanagustin/conduit/internal/observability/domain"
	"github.com/jonathanagustin/conduit/internal/observability/ports"
)

type workerTrace struct {
	operation       ports.Operation
	parent, current obs.TraceContext
	ended           bool
}
type workerTestTracer struct{ spans []*workerTrace }

func (tracer *workerTestTracer) Start(ctx context.Context, operation ports.Operation, parent obs.TraceContext) (context.Context, ports.Span) {
	span := &workerTrace{operation: operation, parent: parent}
	current := parent
	if !current.Valid() {
		current, _ = obs.TraceFromContext(ctx)
	}
	if current.Valid() {
		current = current.Child()
	} else {
		current = obs.NewTraceContext()
	}
	span.current = current
	tracer.spans = append(tracer.spans, span)
	return ports.ContextWithSpan(obs.ContextWithTrace(ctx, current), span), span
}
func (*workerTrace) HTTP(string, string, int) {}
func (*workerTrace) Link(obs.TraceContext)    {}
func (span *workerTrace) End(bool)            { span.ended = true }

type tracedControlPlane struct {
	*fakeControlPlane
	start, finish obs.TraceContext
}

func (control *tracedControlPlane) Start(ctx context.Context, e, a, p, token string) error {
	control.start, _ = ports.RecordedTrace(ctx)
	return control.fakeControlPlane.Start(ctx, e, a, p, token)
}
func (control *tracedControlPlane) Finish(ctx context.Context, e, a, p, token string, success bool, failure, installation string, execution controlplane.PlanExecution, report controlplane.EnvironmentReport) error {
	control.finish, _ = ports.RecordedTrace(ctx)
	return control.fakeControlPlane.Finish(ctx, e, a, p, token, success, failure, installation, execution, report)
}

func TestWorkerContinuesPersistedDecisionThroughStartApplyAndReport(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "persisted parent", true: "malformed parent"}[malformed], func(t *testing.T) {
			parent := obs.NewTraceContext()
			record := leasedRecord()
			record.DecisionTraceparent = parent.Traceparent()
			if malformed {
				record.DecisionTraceparent = "not-a-trace"
			}
			control := &tracedControlPlane{fakeControlPlane: &fakeControlPlane{record: record}}
			tracer := &workerTestTracer{}
			worker := Worker{ControlPlane: control, Executor: &fakeExecutor{}, Tracer: tracer, Environment: "dev", AgentID: "agent", LeaseDuration: time.Minute}
			worked, err := worker.RunOnce(context.Background())
			if err != nil || !worked {
				t.Fatalf("run = %v, %v", worked, err)
			}
			if len(tracer.spans) != 4 {
				t.Fatalf("got %d spans, want lease, execution and two applies", len(tracer.spans))
			}
			execution := tracer.spans[1]
			if execution.operation != ports.AgentExecution {
				t.Fatal("missing execution span")
			}
			if malformed {
				if execution.parent.Valid() {
					t.Fatal("malformed context was inherited")
				}
			} else if execution.parent != parent || execution.current.TraceID != parent.TraceID {
				t.Fatal("decision trace was not continued")
			}
			if control.start != execution.current || control.finish != execution.current {
				t.Fatal("start or final report lost execution context")
			}
			for _, span := range tracer.spans {
				if !span.ended {
					t.Fatal("span not ended")
				}
				if span.operation == ports.AgentApply && span.current.TraceID != execution.current.TraceID {
					t.Fatal("apply lost execution trace")
				}
			}
		})
	}
}
