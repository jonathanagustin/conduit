# Hub request rate limiting

Issue #842. The Hub serves operators and Agents on one listener and, before
this, had no bound on how fast any caller could ask it for work. One operator
script in a loop, or one unauthenticated flood, spent the same goroutines,
database connections and CPU an Agent needs to take a lease and report a Plan.

The Hub now puts a token bucket in front of every route. It always runs: there
is no variable that turns it off.

## What is limited, and by what key

The limiter is the outermost HTTP handler (`composition.RateLimit`, wired in
`cmd/platform-delivery/main.go`). It decides who is asking the same way the
routes beneath it do, then spends one token from that caller's bucket.

| Class | Who | Bucket key |
| --- | --- | --- |
| `agent` | An Agent identity (mTLS/SPIFFE certificate, or the development headers when those are enabled) on a route the route register (`HubRouteAuthorization`) marks `agent`: leases, heartbeat, start, finish, health targets and observations, maintenance admission, Apollo status reports. | Environment and Agent ID |
| `operator` | Operator claims that resolve (OIDC bearer token, operator certificate, or the development headers) on any other registered, non-public route. | The authenticated username, with service accounts kept apart from users. A delegated on-behalf-of name is not the key. |
| `anonymous` | Everything else: no credential, a credential that does not resolve, a public route, or a path the Hub does not serve. | The TCP peer address; an IPv6 peer is keyed by its /64. |

Classes share no state. Operator or anonymous traffic, however heavy, cannot
spend an Agent's tokens, and one caller cannot spend another's.

`GET /health/live` and `GET /health/ready` are exempt. A probe refused for load
would restart a Hub for being busy.

A request is counted once, when it arrives. A long-lived response — the
live-event stream — costs one token to open and is never throttled while open.

A request over its limit receives `429 Too Many Requests`, a `Retry-After`
header in whole seconds (never `0`), and the Hub's JSON error body
(`{"error": "...", "status": "429"}`). A refusal spends no token, so waiting
`Retry-After` is always enough to be served once.

### Forwarded headers are not trusted

The anonymous key is the address of the TCP connection. `X-Forwarded-For`,
`Forwarded` and `X-Real-IP` are **not** read: any client can write them, so
honouring them would let a caller choose a fresh bucket for every request.

The consequence, when the Hub sits behind a gateway or load balancer that
terminates connections: every unauthenticated request arrives from the
gateway's address and shares **one** anonymous bucket. Authenticated operators
and Agents are unaffected, because they are keyed by identity. Either raise
`RATE_LIMIT_ANONYMOUS_RPS`/`_BURST` to the unauthenticated load the gateway is
expected to pass, or limit anonymous traffic at the gateway, which knows the
client. There is no setting that makes the Hub trust a forwarding header.

Under `ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS` or
`ALLOW_INSECURE_AGENT_IDENTITY_HEADERS` the identity, and therefore the bucket,
is whatever the caller's headers say. Those modes are for development and the
limiter is not a control there.

## Configuration

All optional. A value that is set and unusable stops the Hub at startup rather
than falling back to the default.

| Variable | Default | Meaning |
| --- | --- | --- |
| `RATE_LIMIT_OPERATOR_RPS` | `100` | Sustained requests per second for one operator. |
| `RATE_LIMIT_OPERATOR_BURST` | `600` | Requests one operator may make at once. |
| `RATE_LIMIT_AGENT_RPS` | `20` | Sustained requests per second for one Agent. |
| `RATE_LIMIT_AGENT_BURST` | `60` | Requests one Agent may make at once. |
| `RATE_LIMIT_ANONYMOUS_RPS` | `10` | Sustained requests per second for one remote address. |
| `RATE_LIMIT_ANONYMOUS_BURST` | `60` | Requests one remote address may make at once. |

`*_RPS` is a positive number (fractions allowed) up to 1,000,000; `*_BURST` is
a whole number from 1 to 1,000,000.

### Why these defaults

They are a ceiling on one caller, generous enough that no shipped client
reaches it, not a capacity plan for the Hub.

- **Agent.** `platform-spoke-agent` polls for a lease every `POLL_INTERVAL`
  (default 15 s) and reports health every `HEALTH_POLL_INTERVAL` (default and
  minimum 5 s): about 0.27 requests per second idle. The simulated agent polls
  every 2 s: about 0.7. Start, heartbeat and finish add a handful per Plan.
  20 per second with a burst of 60 is more than twenty times the busiest of
  those, per Agent.
- **Operator.** One console refresh (`web/src/console/operator/live-hub.js`)
  reads every index one request after another: about twenty fixed reads, up to
  five pages for each paginated index, and two or more reads per packaging run
  in the first page of runs. That is a few hundred requests in sequence for a
  large estate, which a burst of 600 absorbs; 100 per second sustains
  back-to-back refreshes. The live-event stream costs one token per open. CLI
  and CI service accounts are separate principals with their own buckets.
- **Anonymous.** The console shell and its bundled assets, the sign-in
  discovery read, and a retry or two fit inside 60. It is the strictest class
  because it is the only one a caller enters without proving who they are.
  See the gateway note above before relying on it behind a proxy.

## Memory bound

The limiter is a table keyed by something a caller influences, so the table is
bounded: at most 10,000 callers per class. A caller's bucket is forgotten after
ten minutes idle (or after its full refill time, if longer, so forgetting never
grants an early burst). When a class's table is full, further new callers share
one overflow bucket of that class's shape until idle entries are swept:
inventing identities or addresses cannot grow the Hub's memory or buy extra
tokens. These two bounds are not configurable.

## Setting the limits in a deployment

- **Helm:** `rateLimit.operator`, `rateLimit.agent` and `rateLimit.anonymous`
  on the Hub chart, each with `rps` and `burst`. They are `null` by default,
  which renders no variable, so the defaults above apply. The chart schema
  holds `rps` to a number above 0 and `burst` to a whole number from 1, both at
  most 1000000, and has no key that disables the limiter.
- **Zarf:** the native package values of the same names as the environment
  variables, empty by default. An empty value is read as unset.

## Limits of this control

- Buckets are per Hub replica and in memory. With N replicas behind a load
  balancer a caller's effective ceiling is up to N times the configured one,
  and a restart resets every bucket.
- It bounds request rate, not concurrency or cost. There is no connection cap
  and no per-route weight; one expensive read costs one token like a cheap one.
- A token is spent after the credential is examined, because the key depends on
  it. A flood of invalid bearer tokens is limited as anonymous traffic but each
  request still costs one verification attempt.

## Metrics

On the opt-in metrics listener (`METRICS_ADDRESS`, PostgreSQL repository only):

- `conduit_rate_limit_decisions_total{class, outcome}` — counter;
  `outcome` is `allowed` or `limited`.
- `conduit_rate_limit_tracked_callers{class}` — gauge; buckets currently held.

`class` is `operator`, `agent` or `anonymous`: a closed set fixed in code. No
principal, Agent, Environment or address is ever a label value, and the
exporter refuses a class outside that set.
`rate(conduit_rate_limit_decisions_total{outcome="limited"}[5m]) > 0` for
`class="agent"` means the Agent limit is too low for the fleet's polling.

`ConduitAgentRateLimited` in `deploy/observability/prometheus-rules.yaml` is
that expression as an alert; its runbook is
[`runbooks.md`](runbooks.md#conduitagentratelimited).

Source anchors: `internal/hub/ratelimit`, `internal/hub/composition/rate_limit.go`,
`cmd/platform-delivery/rate_limit.go`, `internal/observability/application/ratelimit.go`.

## Executable startup verification

`cmd/platform-delivery/rate_limit_startup_test.go` starts the real Hub entrypoint
in a child test process with explicit development identity and signing opt-ins.
It checks configured bursts, per-principal and per-Agent isolation, anonymous
refusals on an unregistered route, positive `Retry-After`, exempt probes, and
startup refusal of an invalid limit. An admitted Agent poll returns the real
lease handler's empty work queue; this is not evidence of a granted lease or
executed Plan.

`TestPostgresHubRequestLimiterEntrypointMetrics` runs in the PostgreSQL integration
gate, uses a private schema, and reads the separate metrics listener after the
same traffic. It checks exact class counters and absence of caller identifiers.
These local process checks establish bootstrap wiring, not production capacity,
TLS authentication, or multi-replica quota enforcement.
