package domain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

const (
	traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	spanID  = "00f067aa0ba902b7"
)

func TestAWellFormedTraceparentIsRead(t *testing.T) {
	t.Parallel()
	trace, ok := domain.ParseTraceparent("00-" + traceID + "-" + spanID + "-01")
	if !ok || trace.TraceID != traceID || trace.SpanID != spanID || !trace.Sampled {
		t.Fatalf("parsed %+v ok=%v", trace, ok)
	}
	if trace.Traceparent() != "00-"+traceID+"-"+spanID+"-01" {
		t.Fatalf("round trip gave %q", trace.Traceparent())
	}
	unsampled, ok := domain.ParseTraceparent("00-" + traceID + "-" + spanID + "-00")
	if !ok || unsampled.Sampled {
		t.Fatalf("flags 00 parsed as %+v ok=%v", unsampled, ok)
	}
	// A later version is read by its first four fields.
	future, ok := domain.ParseTraceparent("01-" + traceID + "-" + spanID + "-01-extra")
	if !ok || future.TraceID != traceID {
		t.Fatalf("a later version was refused: %+v ok=%v", future, ok)
	}
}

// The value is logged and sent onward, so anything that is not exactly the
// specified shape is treated as absent.
func TestAMalformedTraceparentIsTreatedAsAbsent(t *testing.T) {
	t.Parallel()
	for name, header := range map[string]string{
		"empty":                    "",
		"uppercase hex":            "00-" + strings.ToUpper(traceID) + "-" + spanID + "-01",
		"an all-zero trace id":     "00-" + strings.Repeat("0", 32) + "-" + spanID + "-01",
		"an all-zero span id":      "00-" + traceID + "-" + strings.Repeat("0", 16) + "-01",
		"version ff":               "ff-" + traceID + "-" + spanID + "-01",
		"a short trace id":         "00-" + traceID[:30] + "-" + spanID + "-01",
		"version 00 with a suffix": "00-" + traceID + "-" + spanID + "-01-extra",
		"a later version, glued":   "01-" + traceID + "-" + spanID + "-01extra",
		"wrong separators":         "00_" + traceID + "_" + spanID + "_01",
		"non-hex flags":            "00-" + traceID + "-" + spanID + "-zz",
		"an injected line":         "00-" + traceID[:20] + "\nforged=true-" + spanID + "-01",
	} {
		if trace, ok := domain.ParseTraceparent(header); ok {
			t.Errorf("%s was accepted as %+v", name, trace)
		}
	}
}

func TestANewTraceIsValidAndAChildKeepsTheTraceAndChangesTheSpan(t *testing.T) {
	t.Parallel()
	trace := domain.NewTraceContext()
	if !trace.Valid() || trace.TraceID == domain.NewTraceContext().TraceID {
		t.Fatalf("a minted trace is invalid or repeated: %+v", trace)
	}
	child := trace.Child()
	if child.TraceID != trace.TraceID || child.SpanID == trace.SpanID || !child.Valid() {
		t.Fatalf("child %+v of %+v", child, trace)
	}
	if _, ok := domain.ParseTraceparent(child.Traceparent()); !ok {
		t.Fatalf("a rendered traceparent does not parse: %q", child.Traceparent())
	}
}

func TestTheOutboundHeaderIsAChildSpanAndAbsentWithoutATrace(t *testing.T) {
	t.Parallel()
	if header := domain.OutboundTraceparent(context.Background()); header != "" {
		t.Fatalf("a context with no trace produced %q", header)
	}
	if header := domain.OutboundTraceparent(domain.ContextWithTrace(context.Background(), domain.TraceContext{})); header != "" {
		t.Fatalf("an invalid trace was sent onward as %q", header)
	}
	trace := domain.TraceContext{TraceID: traceID, SpanID: spanID, Sampled: true}
	sent, ok := domain.ParseTraceparent(domain.OutboundTraceparent(domain.ContextWithTrace(context.Background(), trace)))
	if !ok || sent.TraceID != traceID || sent.SpanID == spanID || !sent.Sampled {
		t.Fatalf("sent %+v ok=%v", sent, ok)
	}
}

func TestARequestIDIsBoundedInLengthAndCharacterSet(t *testing.T) {
	t.Parallel()
	for _, accepted := range []string{"a", "01HZX3K9-req.7_b", strings.Repeat("a", 64), "3f2c1e0a-9b1d-4c5e-8f70-112233445566"} {
		if !domain.ValidRequestID(accepted) {
			t.Errorf("%q was refused", accepted)
		}
	}
	for _, refused := range []string{"", strings.Repeat("a", 65), "a b", "a\nb", "a\"b", "a/b", "a=b", "a;b", "café", "a\x00b"} {
		if domain.ValidRequestID(refused) {
			t.Errorf("%q was accepted", refused)
		}
	}
	if minted := domain.NewRequestID(); !domain.ValidRequestID(minted) || minted == domain.NewRequestID() {
		t.Errorf("a minted request id is invalid or repeated: %q", minted)
	}
}
