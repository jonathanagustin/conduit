# Hub alert runbooks

One section per alert in
[`deploy/observability/prometheus-rules.yaml`](../../deploy/observability/prometheus-rules.yaml).
Each alert's `runbook_url` points at its section here, and
`scripts/verify-observability-assets.rb` fails if an alert has no section, a
section has no alert, or a section lacks one of its five parts.

These runbooks grant nothing. A step that changes a deployment, a database row
or a limit needs the authority that change would need on any other day.

## Conventions

The commands assume the Zarf package's names: namespace `platform-system`,
Deployment `platform-delivery`, container `delivery-control-plane`. A Helm
install names the Deployment after the release; substitute it, or select with
`-l app.kubernetes.io/component=hub`.

- **Metrics** are on the Hub's own listener (`METRICS_ADDRESS`, port 9090 in the
  package), plain HTTP, PostgreSQL repository only:

  ```sh
  kubectl -n platform-system port-forward deploy/platform-delivery 9090:9090
  curl -s localhost:9090/metrics | grep '^conduit_'
  ```

- **The access log** is one line per request, at INFO with the message `request`, on the Hub's
  standard error, with `method`, `route` (the registered pattern, never the
  path), `status`, `duration_seconds`, `caller` (`operator`, `agent` or
  `anonymous`), `request_id` and `trace_id`. It carries no path, query string,
  header or principal name, so it cannot tell you *which* Environment or
  operator; the audit trail (`GET /v1/authorization-audit`) does that.

  ```sh
  kubectl -n platform-system logs deploy/platform-delivery -c delivery-control-plane --since=15m | grep ' INFO request '
  ```

- **SQL** is run against the Hub's PostgreSQL database with the DSN in the
  Secret the deployment names (`PACKAGING_POSTGRES_SECRET`, key `dsn`). Every
  query below is a `SELECT`. The tables hold Environment and installation
  names; treat the output as tenant data.

- The queue and delivery gauges are read from the database, so every replica
  reports the same value. The request and rate-limit counters are per replica.

## ConduitHubWedged

### Meaning

Work is due in a reconciliation queue, no replica holds a lease on any of it,
and the oldest due item has waited more than five minutes. Deployments that
were decided are not being carried out. A deep queue with leases and a young
oldest item is a busy Hub and does not fire this.

### First checks

```promql
max by (queue) (conduit_reconciliation_pending)
max by (queue) (conduit_reconciliation_leased)
max by (queue) (conduit_reconciliation_oldest_pending_seconds)
count by (version) (conduit_build_info)
```

```sh
kubectl -n platform-system get pods -l app.kubernetes.io/name=platform-delivery
kubectl -n platform-system logs deploy/platform-delivery -c delivery-control-plane --since=15m \
  | grep -E 'durable reconciliation cycle| ERROR '
```

```sql
SELECT count(*) FILTER (WHERE lease_expires_at > now()) AS leased,
       count(*) FILTER (WHERE NOT exhausted AND available_at <= now()) AS due,
       min(available_at) FILTER (WHERE NOT exhausted AND available_at <= now()) AS oldest_due
FROM conduit_reconciliation_work;
```

### Likely causes

- The reconciliation supervisor is erroring every cycle (`durable
  reconciliation cycle` in the log, with the error): the database is refusing
  writes, a migration is half applied, or a connection pool is exhausted.
- No Hub replica is running the supervisor: all replicas are crash-looping, or
  the running ones were started with `REPOSITORY_KIND=file`.
- The database clock and the Hubs' clocks disagree badly. Leasing compares
  `available_at` with the Hub's clock.

### Remediation

Fix what the log names. If the supervisor logs nothing and the pods are
`Running`, restart one replica (`kubectl -n platform-system rollout restart
deploy/platform-delivery`) and watch `conduit_reconciliation_leased`: leases
are in the database and expire on their own, so a restarted replica resumes
without any row being edited. The alert clears when a lease is taken.

### What not to do

- Do not edit or delete rows in `conduit_reconciliation_work` to "unstick" it.
  A lease that looks stale expires by itself; deleting the row discards the
  decision it stands for.
- Do not scale the Hub up as a first response. If one replica cannot take a
  lease, a third will not either, and each adds database connections.
- Do not treat depth as the problem. Silencing this because "the queue is
  only 3 deep" misses that 3 with no lease is the wedge.

## ConduitReconciliationLag

### Meaning

The oldest due item in a queue has waited more than fifteen minutes for ten
minutes running. Unlike `ConduitHubWedged`, replicas may be taking work: the
Hub is behind, not stopped.

### First checks

```promql
max by (queue) (conduit_reconciliation_oldest_pending_seconds)
max by (queue) (conduit_reconciliation_pending)
max by (queue) (conduit_reconciliation_leased)
histogram_quantile(0.95, sum by (le) (rate(conduit_http_request_duration_seconds_bucket[5m])))
```

If `leased` is zero, this is `ConduitHubWedged` about to fire; use that
section. Otherwise check whether the backlog is growing or draining over the
last hour, and whether the database is slow (the request p95 rises with it).

### Likely causes

- A burst: many Environments changed at once (a fleet-wide Release, a
  recall). The backlog drains on its own.
- A slow database, so each item takes longer than the poll interval.
- Items failing and retrying with backoff, which keeps them due. Look for
  `last_error` values:

  ```sql
  SELECT last_error, count(*) FROM conduit_reconciliation_work
  WHERE NOT exhausted AND attempt > 1 GROUP BY last_error ORDER BY count(*) DESC LIMIT 10;
  ```

### Remediation

A draining burst needs nothing. For a slow database, fix the database. For
repeated failures, fix the cause the `last_error` names; the items retry by
themselves.

### What not to do

- Do not raise retry budgets or shorten backoff to "catch up". They are
  constants in the Hub, and hammering a struggling dependency is how a lag
  becomes an outage.
- Do not restart Hubs repeatedly. Each restart abandons in-flight leases,
  which then wait out their expiry.

## ConduitProposalExhausted

### Meaning

The number of exhausted rows in the proposal outbox rose in the last thirty
minutes. A proposal is how a reconciliation decision becomes a Plan. One of
two things happened, and the metric cannot tell them apart:

- a proposal ran out of retries, so an Environment has a decided change that
  nothing will propose again; or
- a newer desired state superseded a proposal, which the store also records as
  exhausted. This is routine and needs no action.

### First checks

```sql
SELECT last_error, count(*) FROM conduit_reconciliation_proposal_outbox
WHERE exhausted GROUP BY last_error ORDER BY count(*) DESC;
```

`desired snapshot superseded proposal` is the benign case. Anything else is a
proposal that failed until its budget ran out; that text is the reason. The
Hub also raises a notification for a proposal that ran out of retries (and
not for a superseded one), so a subscribed operator's inbox has it.

```sh
kubectl -n platform-system logs deploy/platform-delivery -c delivery-control-plane --since=1h \
  | grep 'durable reconciliation cycle'
```

### Likely causes

- The Plan could not be created: an approval policy, a suppression or a
  maintenance window refused it every time.
- The control-plane repository refused the write for the whole retry budget.

### Remediation

Remove the cause named by `last_error`. The proposal is then produced again
after the installation's desired state, or the Agent's observation of it,
next changes: either resets the installation's work item, which is decided
again. This
repository ships no command that re-queues an exhausted proposal in place.

### What not to do

- Do not set `exhausted = false` by hand. The row's document was built for a
  desired generation that may no longer be current, and replaying it proposes
  a stale Plan.
- Do not delete exhausted rows to clear the alert. It clears by itself thirty
  minutes after the count stops rising, and the rows are the record of why.

## ConduitReconciliationExhausted

### Meaning

At least one installation's reconciliation work item has run out of retries.
The Hub has stopped trying to decide what that installation needs. It stays
that way until a changed desired state or a changed Agent observation is
recorded for it.

### First checks

```promql
max by (queue) (conduit_reconciliation_exhausted{queue="reconciliation"})
```

```sql
SELECT environment, installation, attempt, last_error
FROM conduit_reconciliation_work WHERE exhausted ORDER BY environment, installation;
```

### Likely causes

- The installation's inputs cannot be planned: a Release that no longer
  exists, a dependency that cannot be satisfied, a constraint that excludes
  every candidate.
- A fault that lasted longer than the whole retry budget (ten attempts,
  backing off to an hour).

### Remediation

Fix what `last_error` names, then change the installation's desired state
through the normal route (a new Release on its channel, or the corrected
configuration). A changed desired snapshot, or a changed observation from the
Agent, resets the item's attempts and its exhausted flag.

### What not to do

- Do not reset the row by hand. If the cause is still there it exhausts
  again, and in the meantime the alert has cleared.
- Do not ignore it because the Environment "looks fine". It looks fine
  because nothing is being decided for it.

## ConduitNotificationDeliveryExhausted

### Meaning

In the last thirty minutes the Hub gave up delivering at least one
notification by the named kind of transport. A person who subscribed to be
told about a failure was not told. The inbox entry still exists and is
readable in the console; what failed is the delivery out of the Hub.

### First checks

```promql
max by (transport) (conduit_notification_delivery_exhausted)
sum by (transport) (rate(conduit_notification_delivery_failures_total[15m]))
sum by (transport) (rate(conduit_notification_delivery_successes_total[15m]))
```

```sql
SELECT channel, left(delivery_error, 200) AS reason, count(*)
FROM conduit_notification_inbox WHERE delivery_exhausted
GROUP BY channel, reason ORDER BY count(*) DESC LIMIT 20;
```

The reason begins with the transport's name. It does not contain the
recipient's address.

### Likely causes

- A permanent refusal on the first attempt: the webhook answered 404 or 410,
  the SMTP relay rejected the sender, the Slack token was revoked. These
  exhaust immediately, without spending retries.
- A recipient with no contact for the channel they chose (native delivery).
- An outage longer than the retry budget.

### Remediation

Correct the transport configuration or the credential it reads and roll the
Hub. New notifications then deliver. Exhausted ones are not retried: tell the
affected recipients by another route, using the inbox as the list of what
they missed. This repository ships no command that re-queues an exhausted
delivery.

### What not to do

- Do not clear `delivery_exhausted` by hand to resend. Delivery is
  at-least-once and the entry may be hours old; a late "Plan failed" for a
  Plan that has since succeeded is worse than none.
- Do not paste a recipient's address, a webhook URL or a token into the
  incident channel while investigating. The log and the metric deliberately
  carry none of them.

## ConduitNotificationDeliveryStalled

### Meaning

The oldest undelivered notification for a kind of transport was raised more
than thirty minutes ago. Deliveries are failing and backing off, or nothing is
attempting them. Nothing has been given up on yet.

### First checks

```promql
max by (transport) (conduit_notification_delivery_pending)
max by (transport) (conduit_notification_delivery_oldest_pending_seconds)
sum by (transport) (rate(conduit_notification_delivery_attempts_total[15m]))
```

Attempts at zero with a backlog means no replica is running the dispatcher.
Attempts above zero with no successes means the transport is refusing.

```sh
kubectl -n platform-system logs deploy/platform-delivery -c delivery-control-plane --since=30m \
  | grep -E 'notification delivery'
```

### Likely causes

- The endpoint, relay or Slack API is down or slow.
- An egress rule does not allow the Hub to reach it.
- The notification store is failing (`notification delivery cycle` errors).

### Remediation

Restore the transport or the egress rule. Pending entries retry on their own
schedule, up to an hour apart; nothing needs to be re-queued.

### What not to do

- Do not switch transport to make the backlog move. Entries were queued for a
  channel, and a different transport may send them somewhere the recipient
  did not choose.
- Do not restart the Hub in a loop to force retries. The retry time is in the
  database and a restart does not bring it forward.

## ConduitHTTPServerErrors

### Meaning

More than 5% of the Hub's requests were answered with a 5xx for ten minutes,
at more than one request every ten seconds. Operators and Agents are seeing
failures. A 429 is a 4xx and is not counted here.

### First checks

```promql
topk(10, sum by (route, method) (rate(conduit_http_requests_total{code_class="5xx"}[5m])))
sum by (code_class) (rate(conduit_http_requests_total[5m]))
histogram_quantile(0.99, sum by (le) (rate(conduit_http_request_duration_seconds_bucket[5m])))
```

```sh
kubectl -n platform-system logs deploy/platform-delivery -c delivery-control-plane --since=15m \
  | grep ' INFO request ' | grep -E 'status=5[0-9][0-9]' | tail -50
kubectl -n platform-system logs deploy/platform-delivery -c delivery-control-plane --since=15m | grep ' ERROR '
```

Take a `trace_id` from a failing line and search for it to find every line of
that request across replicas. One route failing points at one feature; every
route failing points at the database or the identity provider.

### Likely causes

- The database is unavailable or slow; durations climb to the write timeout.
- A rollout: two versions in `conduit_build_info`, with one failing.
- A dependency of one route (the model endpoint, a registry, Temporal).

### Remediation

If it began with a rollout, roll back the Deployment. Otherwise restore the
dependency the failing routes share. Agents retry leases and reports by
themselves once the Hub answers.

### What not to do

- Do not raise rate limits. Refusals are not in this ratio, and more traffic
  into a failing Hub makes it worse.
- Do not look for the failing tenant in the access log. It has no path by
  design; use the route pattern to find the feature and the audit trail to
  find the caller.

## ConduitAgentRateLimited

### Meaning

The Hub has refused Agent-class requests with 429 continuously for ten
minutes. An Agent that is refused cannot lease, heartbeat or report, so its
Environment's Plans stall and its leases can expire mid-work.

### First checks

```promql
sum by (class, outcome) (rate(conduit_rate_limit_decisions_total[5m]))
max by (class) (conduit_rate_limit_tracked_callers)
sum by (route) (rate(conduit_http_requests_total{code_class="4xx"}[5m]))
```

The limit is per Agent and per Hub replica. Which Agent is not in the metrics
(it would be a tenant identifier). The Agents' own logs, which record the
failed control-plane call, identify it.

### Likely causes

- `RATE_LIMIT_AGENT_RPS`/`_BURST` were set below what the fleet's poll and
  heartbeat intervals need.
- One Agent is misconfigured with a very short poll interval, or is
  crash-looping and re-leasing on each start.
- Several Agent processes share one Environment and Agent identity, and so
  one bucket.

### Remediation

Fix the Agent that is over-asking. If the limit is simply too low for a
correctly configured fleet, raise `rateLimit.agent.rps` and `.burst` in the
Hub chart (or the `RATE_LIMIT_AGENT_*` package variables) and roll the Hub;
see [`hub-rate-limiting.md`](hub-rate-limiting.md) for how the defaults were
sized.

### What not to do

- Do not look for a way to turn the limiter off. There is none.
- Do not raise the operator or anonymous limits. They are separate classes
  and do not affect Agents.
- Do not raise the Agent limit by orders of magnitude to silence the alert.
  It is the bound that stops one faulty Agent from starving the others.

## ConduitMetricsAbsent

### Meaning

No Hub replica has exported `conduit_build_info` for ten minutes. Every other
alert in this file reads series from the same endpoint, so none of them can
fire. This says nothing about whether the Hub is serving; it says nobody can
tell.

### First checks

```sh
kubectl -n platform-system get pods -l app.kubernetes.io/name=platform-delivery
kubectl -n platform-system get deploy platform-delivery \
  -o jsonpath='{range .spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}' \
  | grep -E '^(METRICS_ADDRESS|REPOSITORY_KIND)='
kubectl -n platform-system logs deploy/platform-delivery -c delivery-control-plane | grep -i metrics
kubectl -n platform-system port-forward deploy/platform-delivery 9090:9090 &
curl -s -o /dev/null -w '%{http_code}\n' localhost:9090/metrics
```

In Prometheus, look at the target's state and last scrape error.

### Likely causes

- The Hub is down.
- `METRICS_ADDRESS` is empty, or `REPOSITORY_KIND` is `file`. The Hub logs
  `METRICS_ADDRESS is set but the repository reports no queue statistics` in
  the second case and opens no listener.
- The endpoint answers 500: the database read behind the gauges is failing
  (`gathering metrics failed`), or a metric was refused on the publishing
  path (`refusing to publish an invalid metrics registry`). The endpoint never
  serves a partial payload.
- The scrape cannot reach port 9090: a missing ServiceMonitor, a network
  policy, or the `uds` component selected where `uds-metrics` was meant.

### Remediation

Restore whichever of those is missing. A 500 from a failing database read
clears when the database does. `refusing to publish an invalid metrics
registry` is a defect in the build; roll back.

### What not to do

- Do not silence this as "only monitoring". While it fires, a wedged Hub and
  a healthy one look the same.
- Do not expose the metrics port through the tenant gateway to make it
  reachable. It is unauthenticated plain HTTP on its own listener on purpose.
