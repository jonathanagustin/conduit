package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Correlation without a tracing library.
//
// docs/decisions/0018-tracing.md records why there is no OpenTelemetry SDK
// here yet. What this file gives in the meantime is the part of tracing that
// needs no exporter: every request has a W3C trace id that the caller may
// supply and the Hub otherwise mints, the id is written on every access-log
// line, and it is sent on to whatever the request calls. That is enough to
// answer "which log lines, on which processes, belong to this one request",
// and it is forward compatible: the header is the one an OTLP pipeline reads.

// TraceparentHeader is the W3C Trace Context request header.
const TraceparentHeader = "traceparent"

// RequestIDHeader carries the id a caller can quote back to an operator.
const RequestIDHeader = "X-Request-Id"

// TraceContext is one position in a trace: the trace and the span that is
// current at that position.
type TraceContext struct {
	TraceID string // 32 lowercase hex characters, not all zero
	SpanID  string // 16 lowercase hex characters, not all zero
	Sampled bool
}

// Valid reports whether both ids have the W3C shape.
func (trace TraceContext) Valid() bool {
	return lowerHex(trace.TraceID, 32) && lowerHex(trace.SpanID, 16)
}

// Traceparent renders the header value, version 00.
func (trace TraceContext) Traceparent() string {
	flags := "00"
	if trace.Sampled {
		flags = "01"
	}
	return "00-" + trace.TraceID + "-" + trace.SpanID + "-" + flags
}

// ParseTraceparent reads a traceparent header.
//
// Strict, because the value is written into logs and sent onward: anything
// that is not exactly the specified shape is treated as absent and a new trace
// is started, rather than a caller-chosen string being carried through the
// system under the name "trace_id". Per the specification, version ff is
// invalid, version 00 has exactly four fields, and a later version is read by
// its first four.
func ParseTraceparent(header string) (TraceContext, bool) {
	header = strings.TrimSpace(header)
	if len(header) < 55 {
		return TraceContext{}, false
	}
	version := header[:2]
	if !lowerHex(version, 0) || version == "ff" {
		return TraceContext{}, false
	}
	if version == "00" && len(header) != 55 {
		return TraceContext{}, false
	}
	if len(header) > 55 && header[55] != '-' {
		return TraceContext{}, false
	}
	if header[2] != '-' || header[35] != '-' || header[52] != '-' {
		return TraceContext{}, false
	}
	flags := header[53:55]
	if !lowerHex(flags, 0) {
		return TraceContext{}, false
	}
	trace := TraceContext{TraceID: header[3:35], SpanID: header[36:52]}
	if !trace.Valid() {
		return TraceContext{}, false
	}
	// The sampled bit is the lowest of the flags byte.
	trace.Sampled = strings.ContainsRune("13579bdf", rune(flags[1]))
	return trace, true
}

// NewTraceContext starts a trace.
func NewTraceContext() TraceContext {
	return TraceContext{TraceID: randomHex(16), SpanID: randomHex(8)}
}

// Child is a new span in the same trace: what a process sends onward, so the
// receiver's parent is this process and not this process's caller.
func (trace TraceContext) Child() TraceContext {
	return TraceContext{TraceID: trace.TraceID, SpanID: randomHex(8), Sampled: trace.Sampled}
}

// maximumRequestIDLength bounds an inbound request id. Long enough for a UUID,
// a ULID or a gateway's own id; short enough that a log line cannot be
// inflated by a header.
const maximumRequestIDLength = 64

// ValidRequestID reports whether a caller-supplied request id may be honoured.
//
// The id is echoed in a response header and written to the log, so it is held
// to letters, digits, '.', '_' and '-'. That excludes everything that could
// break a log line or a header -- spaces, quotes, control characters, CR and
// LF -- without a denylist that has to anticipate them.
func ValidRequestID(candidate string) bool {
	if candidate == "" || len(candidate) > maximumRequestIDLength {
		return false
	}
	for _, character := range candidate {
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-':
		default:
			return false
		}
	}
	return true
}

// NewRequestID mints a request id: 128 random bits, hex.
func NewRequestID() string { return randomHex(16) }

type traceContextKey struct{}

// ContextWithTrace carries a trace position on a context.
func ContextWithTrace(ctx context.Context, trace TraceContext) context.Context {
	return context.WithValue(ctx, traceContextKey{}, trace)
}

// TraceFromContext reads the trace position, if a valid one is carried.
func TraceFromContext(ctx context.Context) (TraceContext, bool) {
	trace, found := ctx.Value(traceContextKey{}).(TraceContext)
	return trace, found && trace.Valid()
}

// OutboundTraceparent is the header value to send on a call made on behalf of
// ctx, or "" when ctx carries no trace. It is a child span, so two outbound
// calls from one request are distinguishable by the receiver.
func OutboundTraceparent(ctx context.Context) string {
	trace, found := TraceFromContext(ctx)
	if !found {
		return ""
	}
	return trace.Child().Traceparent()
}

// lowerHex reports whether text is lowercase hex, of exactly length characters
// when length is positive, and -- for an id -- not all zero.
func lowerHex(text string, length int) bool {
	if length > 0 && len(text) != length {
		return false
	}
	nonZero := length == 0
	for _, character := range text {
		switch {
		case character >= '0' && character <= '9', character >= 'a' && character <= 'f':
		default:
			return false
		}
		if character != '0' {
			nonZero = true
		}
	}
	return nonZero
}

func randomHex(bytes int) string {
	buffer := make([]byte, bytes)
	// crypto/rand.Read does not fail on a supported platform; it panics.
	_, _ = rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
