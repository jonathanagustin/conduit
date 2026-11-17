# ADR 0018: Tracing — W3C trace context now, OTLP export as an owner decision

Status: **Proposed** (2026-11-17), issue #839. The first half is implemented
and described below as it stands. The second half — whether to add the
OpenTelemetry SDK and export spans over OTLP — is not decided, and nothing in
the repository assumes it will be.

## Context

Issue #839 asks for "OTLP tracing across decide, lease and report, including
the Agent". `go.mod` names no OpenTelemetry module, direct or indirect.
`go.sum` holds checksums for a few (`go.opentelemetry.io/otel` and its
`metric` and `trace` modules) because gRPC's tests require them; no Conduit
package imports one and none is built into a Conduit binary.

Two things in the repository argue against adding an SDK as part of an
observability change without an explicit decision:

- `internal/observability/domain` states the position the metrics endpoint was
  built on: no client library, because "the cost of the alternative is a
  supply-chain surface for a diagnostic endpoint", and it renders the
  Prometheus text format itself.
- `docs/security/TRUSTED-TECHNOLOGY-BASELINE.md` admits a new technology by
  naming its repository boundary and the deployment-owned facts beside it.
  Tracing has no row. An exporter adds a network egress from the Hub and the
  Spoke Agent to a collector, which is a new entry in the UDS `Package`
  allow-list and a new place a request's attributes are sent.

OTLP export needs, at minimum, `go.opentelemetry.io/otel/sdk` and one exporter.
`otlptracehttp` brings `go.opentelemetry.io/proto/otlp` on top of
`google.golang.org/protobuf`, which is already a direct requirement;
`otlptracegrpc` additionally links `google.golang.org/grpc`, today an indirect
requirement. Both the Hub image and the Spoke Agent image would carry the new
modules, and both SBOMs and vulnerability scans would grow by that tree. The
exact module list has not been resolved with `go mod tidy`; it should be,
before a decision, and attached to it.

## Decision (implemented): propagate W3C trace context with the standard library

- `internal/observability/domain/trace.go` parses and renders the W3C
  `traceparent` header and mints trace and span identifiers. No dependency.
- The Hub (`composition.ObserveRequests`) continues the caller's trace when the
  header is well formed and starts one otherwise. A malformed header is treated
  as absent: the value is written to logs, so it is never carried through
  unparsed. The trace position is placed on the request context.
- Every access-log line carries `trace_id` next to `request_id`.
- The Spoke Agent starts one trace per work cycle
  (`agent.Worker.RunOnce`) and sends `traceparent` on every call of that cycle:
  lease, start, heartbeat, report. The Hub's access-log lines for one Agent
  cycle therefore share a `trace_id`.

No wire contract changed. `traceparent` is an optional HTTP request header; no
JSON document, protobuf message, OpenAPI schema or persisted row gained a
field.

### What this does and does not give

It gives log correlation: every Hub log line for one request, and every Hub
log line for one Agent cycle, can be selected by one identifier, across
replicas.

It does not give:

- **Spans.** Nothing records a start, an end or a parent-child tree, and
  nothing is exported. There is no flame graph and no latency attribution
  inside a request.
- **Decide → lease.** A Plan is decided in one request and leased in another,
  minutes or hours later, by a different caller. Joining them needs the
  deciding trace to be stored with the Plan and returned with the lease. That
  is a field on the persisted Plan and on the lease response — a contract
  change — and it was deliberately not made here. Today the link between a
  decision and its execution is the Plan identifier, which is in the
  audit trail and not in the access log (the access log carries no path).
- **Outbound calls.** `domain.OutboundTraceparent` exists and the Agent client
  uses it. The Hub's own outbound HTTP clients (notification webhook, Slack,
  model endpoint, OCI registry, OIDC discovery) do not send it. The
  notification dispatcher runs outside any request, so it has no trace to
  send; the others are a small follow-up that needs a decision on whether a
  third party should receive the Hub's trace identifiers at all.

## Options for the undecided half

1. **Stay here.** Log correlation only. No new dependency, no new egress.
   Cost: the acceptance criterion "OTLP tracing" stays open.
2. **Add the OpenTelemetry SDK with an OTLP/HTTP exporter, off unless
   configured.** Spans for the request, the repository call and the Agent's
   apply step, exported to a collector the deployment names. Cost: the
   dependency tree above in two images; a collector egress rule in the chart
   and the UDS `Package`; a rule for which attributes may be set, enforced the
   way the metrics label allowlist is, because a span attribute carrying an
   Environment or Product name publishes the same topology a label would; a
   row in the trusted technology baseline.
3. **Emit OTLP/HTTP JSON without the SDK.** The wire format is specified and
   could be rendered the way the Prometheus text format is. Cost: owning a
   batching exporter, retry and back-pressure, which is exactly the part of
   the SDK that is hard to get right; the attribute rule from option 2 is
   still needed.

Carrying the trace from decision to lease (the stored field) is independent of
the choice above and is needed by options 2 and 3 to be useful.

## What is asked of the owner

Choose option 1, 2 or 3, and separately say whether a Plan may store the trace
identifier of the request that decided it. Until then the repository stays at
option 1 and `docs/operations/PRODUCTION-READINESS.md` gap 2 says so.
