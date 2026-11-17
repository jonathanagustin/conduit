# Hub observability

What the Hub says about itself: metrics, the access log, request and trace
identifiers, and the alert rules, dashboard and runbooks built on them. Issue
#839.

Nothing here carries a tenant identifier. No metric label, and no access-log
field, holds an Environment, Product, installation, principal, Agent, address
or request path.

## Metrics

Served as Prometheus text on the Hub's own listener, plain HTTP, when
`METRICS_ADDRESS` is set **and** `REPOSITORY_KIND=postgres`. With the file
repository the Hub logs a warning and opens no listener; that includes the
request metrics, which do not need a database but share the endpoint.

| Series | Type | Labels | Source |
| --- | --- | --- | --- |
| `conduit_build_info` | gauge | `version` | process |
| `conduit_reconciliation_pending`, `_leased`, `_exhausted`, `_oldest_pending_seconds` | gauge | `queue` | database |
| `conduit_rate_limit_decisions_total` | counter | `class`, `outcome` | process |
| `conduit_rate_limit_tracked_callers` | gauge | `class` | process |
| `conduit_http_requests_total` | counter | `route`, `method`, `code_class` | process |
| `conduit_http_request_duration_seconds` | histogram | `route`, `method` | process |
| `conduit_http_requests_in_flight`, `conduit_http_streams_open` | gauge | none | process |
| `conduit_notification_delivery_attempts_total`, `_successes_total`, `_failures_total`, `_exhausted_total` | counter | `transport` | process |
| `conduit_notification_delivery_pending`, `_exhausted`, `_oldest_pending_seconds` | gauge | `transport` | database |

[`deploy/observability/exported-metrics.txt`](../../deploy/observability/exported-metrics.txt)
is the complete list; a Go test fails when it differs from what the code
renders.

**Process** series are counted by each replica and summed by the scraper.
**Database** series are read from the shared store at scrape time, so every
replica reports the same number: aggregate them with `max`, not `sum`.

### Labels

Every label is on an allowlist in `internal/observability/domain`, checked on
the publishing path: a registry carrying any other label is answered with 500
and no metrics. Each value comes from a closed set held by the exporter, which
maps anything else to a catch-all.

- `route` — the **registered route pattern** a request matched, such as
  `/v1/environments/{environmentID}/commands`, from the Hub's route register
  (`HubRouteAuthorization`). Never the request path. A request that matches no
  registered route is `unmatched`.
- `method` — `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`, or
  `other`.
- `code_class` — `1xx` to `5xx`.
- `class` — `operator`, `agent`, `anonymous`.
- `transport` — `webhook`, `log`, `smtp`, `slack`, `in_app`, `unknown`. It is
  derived from the configured transport and the notification's channel.
- `queue`, `outcome`, `version` — as before.

The series count for requests is bounded by routes × methods × status classes.
`TestAThousandDistinctIdentifiersDoNotGrowTheSeriesCount` requests a thousand
distinct identifiers, paths and methods and requires the count not to move.

### What is counted

Every request is counted, including health probes, requests the rate limiter
refuses with 429, and requests for paths the Hub does not serve.

A **stream** — a server-sent-event response, or a connection a handler takes
over — is counted in `conduit_http_requests_total` when it opens, held in
`conduit_http_streams_open` until it closes, and **not** observed in the
duration histogram. A stream lasts as long as a console tab is open; one
observation of that would move every quantile. Its lifetime is on the
`stream closed` log line.

Duration buckets are 5 ms to 30 s. A request that panics is recorded as 500.

### Notification delivery

Exported only when a delivery transport is configured. The counters record
what this replica's dispatcher did, counted when the store accepted the
outcome. A failed attempt that will be retried counts in `_failures_total`;
one that will not counts in both `_failures_total` and `_exhausted_total`.

`conduit_notification_delivery_oldest_pending_seconds` is measured from when
the event was raised, not from its next retry time, so an entry that has been
failing for six hours reads six hours.

`conduit_notification_delivery_exhausted` counts every exhausted row and only
grows; alert on its change, not its value.

A `transport` has no series until it has something to report: the gauges
appear once a notification has been queued for that channel, and the counters
once this replica has attempted a delivery. A Hub that has never queued a
notification exports none of these, which is not the same as zero.

## Access log

One `log/slog` line per request, at Info, message `request`:

| Field | Meaning |
| --- | --- |
| `method` | The request method. |
| `route` | The registered route pattern, or `unmatched`. |
| `status` | The status written. |
| `duration_seconds` | Arrival to completion. |
| `caller` | `operator`, `agent` or `anonymous`: the class the rate limiter decided on. |
| `request_id` | See below. |
| `trace_id` | See below. |

A stream writes `stream opened` and `stream closed` instead. A health probe
answered below 400 is logged at Debug.

Not logged, ever: the path, the query string, the body, any header (so no
`Authorization` and no `Cookie`), the remote address, and the principal's
name. To find who did what, use the audit trail; the access log answers "what
kind of request failed, how often, how slowly".

### Request id

A caller may send `X-Request-Id`. It is used only if it is 1 to 64 characters
of letters, digits, `.`, `_` and `-`; anything else is replaced by a generated
32-character id, not repaired. The id in use is returned in the response's
`X-Request-Id` header.

### Trace id

The Hub reads a W3C `traceparent` request header. A well-formed one is
continued; a malformed or absent one starts a new trace. The Spoke Agent sends
one trace per work cycle, so the Hub's lines for an Agent's lease, start,
heartbeats and finish share a `trace_id`.

This is log correlation, not tracing. No span is recorded or exported, and a
Plan's decision is not linked to its later lease.
[ADR 0018](../decisions/0018-tracing.md) (Proposed) sets out what OTLP export
would cost and asks for the decision.

## Alerts, dashboard, runbooks

| File | What it is |
| --- | --- |
| [`deploy/observability/prometheus-rules.yaml`](../../deploy/observability/prometheus-rules.yaml) | A `PrometheusRule` with nine alerts. |
| [`deploy/observability/dashboard.json`](../../deploy/observability/dashboard.json) | A Grafana dashboard. |
| [`runbooks.md`](runbooks.md) | One section per alert: meaning, first checks, likely causes, remediation, what not to do. |

Both are shipped two ways, as generated copies of those files:

- **Helm:** `metrics.prometheusRule.enabled` and `metrics.dashboard.enabled`
  on the Hub chart. Both require `metrics.enabled`. The dashboard is a
  ConfigMap labelled `grafana_dashboard: "1"`; change
  `metrics.dashboard.labels` if your sidecar selects on something else.
- **Zarf:** the optional `uds-observability` component, selected together
  with `uds-metrics`.

`ruby scripts/verify-observability-assets.rb` (in the Trunk gate) fails when a
rule or panel names a series the Hub does not export, uses a label that is not
on the allowlist, lacks a runbook section, or when a generated copy differs
from its source. `--write` regenerates the copies.

### Limits of these assets

- The expressions have **not** been evaluated by Prometheus or `promtool`; no
  pinned `promtool` is available to the gate. Names, labels, functions and
  structure are checked. Thresholds and `for` durations are judgement, not
  measurement: nobody has tuned them against a running fleet.
- The dashboard has not been loaded into a Grafana instance.
- Neither the PrometheusRule nor the dashboard ConfigMap has been applied to a
  cluster. That UDS Core's Prometheus selects rules, and its Grafana sidecar
  selects dashboards, from the `platform-system` namespace is assumed from the
  kube-prometheus-stack conventions and not verified here.
- `ConduitProposalExhausted` cannot tell a proposal that ran out of retries
  from one a newer desired state superseded: the store marks both exhausted
  and the gauge counts both. Its runbook's first check distinguishes them.

Source anchors: `internal/observability`,
`internal/hub/composition/request_observation.go`,
`internal/hub/composition/notification_metrics.go`,
`cmd/platform-delivery/observability.go`.
