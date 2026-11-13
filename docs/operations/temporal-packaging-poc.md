# Temporal packaging workflow

Operator notes for [ADR 0013](../decisions/0013-temporal-for-packaging-workflows.md).
Temporal runs the packaging workflow. Conduit governance remains the
approval authority. The delivery kernel remains the authority for Plans,
leases, rollout, and observed state. The worker calls `ProposePlan` and
stops. It does not approve, lease, or execute. This is not a production
Temporal deployment and it is not an air-gap claim.

The assistant console does not render the packaging-run routes.
That console is the ACP assistant, and a packaging-run panel there is
#342. The routes are already on the Hub.

## What you are running

| Piece | Pin | License |
| --- | --- | --- |
| Go SDK | `go.temporal.io/sdk` v1.51.0 | MIT |
| API module | `go.temporal.io/api` v1.64.3 | MIT |
| CLI archive | v1.10.0, SHA-256 `521f461f3d108902763bb71e85cd0fd382dc9357b59604ae03da6ae734d16890` | MIT |
| Embedded server | 1.32.2, printed by `temporal --version` | MIT |
| Embedded UI | 2.56.1, printed by `temporal --version` | MIT |
| Dev-server database file | SQLite, via `--db-filename` | Public domain |
| Worker | `cmd/packaging-workflow` in this repository | this repository's license |

The CLI is fetched by `scripts/fetch-temporal-cli.sh` into `.cache/temporal/`,
which is not committed. After that fetch, the recovery test sets
`ExistingPath` and does not download a server. Provisioning still needs
GitHub for the CLI archive and the Go module proxy for the SDK. The
running demo does not use a Temporal Cloud account. `TEMPORAL_API_KEY`
is refused.

Egress at runtime is loopback. The SDK starts the dev server with
`frontend.enableServerVersionCheck=false`. The demo adds
`--ui-disable-news-fetch` and binds `--ip` and `--ui-ip` to `127.0.0.1`.
One inspected process owned only `127.0.0.1` sockets: listeners and
established connections, including the extra local ports the dev server
opens besides the frontend and UI. The network interface was not
disconnected for that inspection.

## Measured cost of the dev server

`TestDevServerRestartRecoversWaitAndHandoff` on 2026-11-13 recorded:

| Measurement | Value |
| --- | --- |
| Time until the dev server accepted a client | 214 ms |
| Worker RSS | 144568 KiB |
| SQLite file after the recovered run | 704512 bytes |

Those numbers are one machine, one run. They are not a capacity plan.
A production cluster is a different cost: several services, a database
with upgrades and backups, authentication, TLS, namespace retention,
and an owner. The CLI prints that the dev server is not for production
use and that it skips HTTP security checks. Do not point a Hub at this
process and call it deployed.

## Small installs

A small or disconnected Conduit install does not run this worker. The
The Hub routes are still there: `GET /v1/packaging-runs/{runID}`,
`GET /v1/packaging-runs`, and `GET /v1/packaging-runs/{runID}/events`.
An authenticated operator gets `configured: false` and an empty page.
Nobody should reimplement Temporal inside the delivery kernel to fill
that gap.

There is no model in this proof. A workflow that can sit on loopback
does not mean a model is available offline.

## Local process

`mise run packaging-server` or `task packaging:server` starts the pinned
dev server on `127.0.0.1:7233` with `--db-filename` and
`--ui-disable-news-fetch`. `task packaging:worker` runs
`cmd/packaging-workflow worker`, which proposes Plans into
`.packaging-state/plans` and journals their ids beside that. Point the
Hub at the same frontend with `PACKAGING_TEMPORAL_ADDRESS=127.0.0.1:7233`
when you want the packaging-run routes to answer with live progress.
Leave that variable unset and the Hub does not dial Temporal. The
index and the step history are read-only. They do not approve, cancel,
or rerun a packaging run. Plan status stays on `GET /v1/plans/{planID}`.

`platform-delivery packaging-worker` is the same worker using
`REPOSITORY_KIND` and `DATA_DIRECTORY` or `POSTGRES_DSN_FILE`, so a
deployed worker shares the Hub's Plan store.

## Demo

The crash drill is the test, not a tour of the UI:

```sh
scripts/fetch-temporal-cli.sh
scripts/packaging-temporal-demo.sh
```

The test does four things in order:

1. Starts the dev server with `--db-filename` and a worker. Assessment
   blocks on a hold file. The test stops the worker, removes the hold,
   and starts a worker again. The run reaches the review wait.
2. Stops the worker and the server. Starts the server on the same
   sqlite file and a new worker. The status query still shows the same
   source digest, proposal revision, and artifact digest. The step
   history query still shows the review wait, and it does not show a
   handoff.
3. Writes an approval record, then signals. The handoff activity calls
   `ProposePlan`, records the Plan id, and blocks. The test stops the
   worker before the activity returns. A new worker opens the Plan
   repository from disk and reconciles. There is one Plan, its status
   is `AWAITING_APPROVAL`, and it has no lease and no execution.
4. Replays the completed history with the same worker code and checks
   the history text for the fixture body and for a private-key marker.

`cmd/packaging-workflow` is the same loop without the kills:
`start-server`, then `worker`, `run`, `approve`, and `status`, using
the frontend address `start-server` prints. `approve` writes the
stand-in governance record before it signals. The signal is not the
approval. `status` prints packaging-run progress. Plan status, once a
real Plan exists, is `GET /v1/plans/{planID}`.

## Failure cases the unit tests cover

`go test ./internal/packagingworkflow/...` does not need the CLI. It
covers a transient propose retry, required lint failure, unknown
security evidence, an unsupported requirement, an activity timeout, a
review-wait timeout, cancel during the wait, cancel racing an approval,
a review signal with no governance record, a changed artifact digest,
an execution-control denial at handoff, a submit that is acknowledged
only on the retry, continue-as-new carrying the assessment, and a
credential marker refused before it becomes workflow input. The index
tests cover an empty page, several pages, a filter change that rejects
the old cursor, a Plan id with no Plan status, and a row hidden from a
caller who cannot read its environment. The history tests cover a
partial journal, a blocked validation, a review wait, a handoff, an
unknown run, an unconfigured worker, a secret-shaped reason, and a
resume cursor that does not add a completion.

`GET /v1/packaging-runs` loads at most 1000 workflow executions from
Temporal visibility and then authorizes that set. A larger index is
refused rather than returned short. With no worker configured the
routes still answer: anonymous callers get 401, and an authenticated
caller gets `configured: false` and an empty page. A configured address
that cannot be dialed is a 502, which is a different case from leaving
the address unset.

The index and the history require `read` on the environment in the
target-plan reference (`environment/entity`). The single-run route
stays available to every authenticated operator. The transport
description is `packaging-runs.openapi.yaml`. It is a Conduit document.
It is not an Apollo contract and it generates no code.

## Packaging the worker

Helm `packagingWorkflow.enabled` and `packagingWorkflow.worker.enabled`
default to false. Turning both on, and setting
`packagingWorkflow.address`, renders a second Deployment from the Hub
image with args `packaging-worker`. It requires `repository.kind=postgres`
so the worker and the Hub share one Plan store. TLS material and an API
key are mounted from secrets the operator already has. The chart does
not deploy a Temporal server.

Zarf component `packaging-worker` is `required: false`. It reuses the Hub
image. `PACKAGING_TEMPORAL_ADDRESS` defaults to `127.0.0.1:7233` and
`PACKAGING_POSTGRES_SECRET` defaults to `platform-postgres`; override
both before deploying the component. The Temporal server is not in the
package. `zarf dev lint` covers the optional component. Nothing in this
repository has deployed it, and the dev server's sqlite file is not the
database that component expects.

## What this does not show

- CI skips the dev-server test unless `CONDUIT_TEMPORAL_CLI` is set.
- No production database, backup restore, TLS, authentication, or
  upgrade was run.
- The handoff journal is a stub. It does not create a Plan in the
  delivery kernel and it does not take a lease.
- One worker. No multi-worker race was run.
- No model and no network call to one.
- The machine's network interface stayed up during the socket
  inspection.
- History retention was not configured. The sqlite file lives until
  it is deleted. A production namespace would set retention; seven
  days is the value to set then, and this demo does not set it.
