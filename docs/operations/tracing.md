# Opt-in distributed tracing

Conduit can export OTLP/HTTP protobuf traces from the Hub and outbound Agent.
Nothing is exported until `CONDUIT_OTLP_HTTP_ENDPOINT` is set. The existing
W3C log-correlation behavior remains available without a collector.
[ADR 0018](../decisions/0018-tracing.md) records the adopted boundary.

## Configure the binaries

| Setting | Meaning | Default |
| --- | --- | --- |
| `CONDUIT_OTLP_HTTP_ENDPOINT` | Complete HTTPS collector endpoint, normally ending in `/v1/traces`; no credentials, query or fragment | Empty; disabled |
| `CONDUIT_TRACE_SAMPLE_RATIO` | Trace-ID sampling probability from 0 through 1 | `0.01` |
| `CONDUIT_OTLP_CA_FILE` | Optional PEM CA file added to system roots | Empty |

Both binaries use the same settings. The collector, TLS identity, availability,
retention and access policy belong to the deployment. No authentication headers
or client-certificate provisioning is supplied by this adapter. Deployments
requiring those features must arrange an admitted collector boundary rather than
put credentials into the endpoint. Standard `OTEL_*` settings do not enable the
exporter or expand the exported attributes. HTTP proxy discovery and redirects
are disabled.

Invalid enabled configuration refuses startup. With an empty endpoint no SDK
provider, exporter or background export worker is created. A ratio of zero keeps
diagnostic continuity without exporting sampled spans.

## Helm and Zarf

Both charts expose `tracing.endpoint`, `tracing.sampleRatio` and
`tracing.caConfigMap.{name,key}`. The CA ConfigMap must already exist in the
release namespace; its selected key is mounted read-only. A CA without an
endpoint is refused.

The native Zarf package exposes:

| Value | Default |
| --- | --- |
| `TRACING_OTLP_HTTP_ENDPOINT` | Empty |
| `TRACING_SAMPLE_RATIO` | `0.01` |
| `TRACING_CA_CONFIGMAP` | Empty; when supplied, the existing ConfigMap's `ca.pem` key is mounted read-only |
| `TRACING_COLLECTOR_NAMESPACE` | Empty |
| `TRACING_COLLECTOR_NAME_LABEL` | Empty |
| `TRACING_COLLECTOR_PORT` | `4318` |

When the endpoint is set, namespace and Pod name-label selection are required.
The optional UDS Package emits separate Hub and Spoke egress entries for exactly
that namespace, label and TCP port. No collector egress is emitted by default.
The endpoint must resolve to those selected collector Pods; port and TLS trust
must agree with the deployment. A collector outside the cluster requires a
separately reviewed deployment network-policy override. No wildcard external
host grant or collector installation is provided.

## What a trace contains

Fixed operations are `hub.request`, `plan.decision`, `agent.lease`,
`agent.execution` and `agent.apply`. A server span carries only registered
`http.route`, bounded `http.request.method` and `http.response.status_code`.
Its resource is the fixed `conduit-hub` or `conduit-agent` service name.

A successful Plan decision persists optional `decisionTraceparent` alongside
the native Plan record, outside its content hash. An exact retry preserves the
original context. Successful lease admission adds a causal link to that context;
polling itself has an independent trace. Agent execution continues the stored
decision's trace, and start, renewal and final-report requests propagate the
actual execution span, without creating a fictitious parent span.

The new record field is native diagnostic metadata, not approval or execution
authority. Old records and clients remain valid. Malformed trace context is
ignored. The exporter drops ambient resources, arbitrary attributes, events,
status messages, instrumentation-scope attributes and link attributes; no raw
path, entity name, body, error message, credential or baggage is exported.

## Operational limits

The nonblocking SDK queue holds at most 256 ended spans and exports batches of
at most 64. Flush interval is five seconds; an export has a three-second timeout.
Retries are disabled and shutdown has a separate five-second budget. Queue
pressure, collector failure or process termination can lose spans. Absence of a
span is not proof that work did not execute, and a sampled trace is not an audit
receipt. Durable lifecycle and authorization records remain authoritative.

To investigate missing traces, check that both binaries have an explicit endpoint,
that the collector's TLS certificate chains to the selected roots, that the
collector and mesh admit the configured source/namespace/label/port, and that the
sampling ratio is appropriate. Do not retry a Plan because a trace is missing.

## Verification scope

Tests use a local TLS collector to decode actual protobuf exports and exercise
ambient-header/resource rejection, bounded attributes, parentage and malformed
configuration. Lifecycle tests cover unchanged Plan identity, idempotent context,
file reopen and lease links; Agent tests cover execution/apply/report context.
The PostgreSQL integration gate includes a fresh-database pool-reopen case.
These are repository test contracts, not evidence of an installed production
collector, real UDS/mesh admission or production sampling/capacity limits.
