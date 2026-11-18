# ADR 0018: Opt-in OTLP/HTTP tracing with bounded disclosure

Status: **Accepted** (2026-11-17), issue #839. The maintainer asked for a
recommendation and authorized implementing it, including Plan decision-to-Agent
trace continuity. The earlier log-correlation-only implementation remains the
default when no collector endpoint is configured.

## Decision

Use the official OpenTelemetry Go SDK and OTLP/HTTP protobuf exporter at a
narrow adapter. Export is disabled by default. Do not build an exporter,
collector, credential store or tracing backend in Conduit.

The integration is owned by `internal/observability/adapters/otlphttp` behind a
bounded tracing port. Each executable owns its provider and bounded shutdown;
no global OpenTelemetry provider or baggage propagator is installed. Hub and
Agent settings and the private collector's trust roots belong to deployments.

The 1.44.0 candidate was rejected by the security gate for GO-2026-6505;
1.45.0 is the advisory-fixed release. No vulnerability exception is used.

Dependencies are pinned in `go.mod`/`go.sum`: OpenTelemetry SDK/exporter 1.45.0,
OTLP protocol 1.11.0 and their resolved dependencies. Admitting this integration
adds tracing to the trusted technology baseline and to both binary build
closures. The existing software-inventory and vulnerability gates remain in
force. It does not authorize publishing images or deploying a collector.

## Data and authority boundary

Only fixed operation names, registered HTTP route patterns, bounded methods and
status codes may be exported. Resources contain only the fixed service name.
No identity, Environment, Product, Plan ID, raw URL, request/response body,
credential, baggage, event, raw error text or ambient resource attribute is
admitted. An exporter wrapper enforces the boundary even if instrumentation
uses the SDK directly; SDK configuration alone is insufficient because its
resource option merges ambient resource attributes.

Destinations must be HTTPS without URL credentials, query strings or fragments.
The exporter uses a private HTTP client, disables redirects and proxy discovery,
sets explicit empty headers, and optionally adds a deployment-owned CA file to
system roots. Standard `OTEL_*` settings cannot enable export or widen its
exported resource/attribute set.

Default sampling is 1%, chosen by trace ID rather than trusting an inbound
sampled bit. The ratio is bounded to [0,1]. Queue size is 256, batches at most
64, export timeout three seconds and shutdown at most five seconds. The SDK's
nonblocking queue can drop telemetry under overload; exporter retries are
disabled. Export failure never retries or authorizes a business operation.

## Asynchronous continuity

The native optional `PlanRecord.decisionTraceparent` field stores the accepted
decision span position only when tracing is configured. It is outside the
hashed Plan and desired-state identity. An idempotent create retains the first
record's context. File and PostgreSQL repositories serialize it with the
record; no new database column or permission is introduced.

A lease request cannot know which Plan it will receive when polling begins.
Its server span therefore links to the stored decision context after successful
lease admission. The Agent starts execution as a child of that stored decision;
start, heartbeat and final-report requests carry its actual span position.
Apply spans are children of execution. This uses causal links for the polling
trace rather than pretending that polling began in the decision's trace.
Malformed context is ignored. Old records without context and old clients
remain usable. Tracing fields never carry lease authority or affect approval,
expiry, retries, fencing, rollback or reported-state checks.

## Deployment and qualification

See [the tracing runbook](../operations/tracing.md) for opt-in configuration,
collector egress, negative controls and evidence limits. A repository test
collector establishes transport and disclosure behavior, not a deployed
collector, mesh admission, Grafana/Tempo integration or fleet-calibrated
sampling. Those remain deployment qualification.

## Alternatives

Log correlation alone avoids an SDK but cannot provide exported spans or
latency attribution. A hand-built OTLP exporter would make Conduit own batching,
back-pressure and retry machinery. Both were rejected in favor of the supported
SDK with a Conduit-owned disclosure boundary.
