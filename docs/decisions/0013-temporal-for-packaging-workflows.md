---
status: accepted
date: 2026-11-13
decision-makers:
  - repository owner
consulted: []
informed: []
---

# Use Temporal for packaging runs, not for Plan execution

## Context and problem statement

Issues #341–#343 describe an agentic application-packaging pipeline:
assessment, a generated proposal, authorized validation, a human review
wait, and a handoff into a Conduit Plan. That pipeline needs retries,
timers, a durable wait, and crash recovery. Building a second copy of
those mechanisms next to the delivery kernel would duplicate the thing
Temporal is for, and putting them inside the kernel would give a workflow
engine authority over approval, leases, rollout, and observed state.

The delivery kernel already owns Plan status, approvals, leases, and
execution outcomes (`internal/controlplane/lifecycle.go`). This decision
does not replace that. It chooses the engine for the packaging run only.

## Decision drivers

- A packaging run must survive a worker or server restart without
  repeating a completed Plan handoff.
- A model retry may return a different proposal. Earlier validation and
  approval must not follow it.
- Required validation failure, unknown security evidence, and unsupported
  requirements must block handoff.
- An incoming Temporal Signal or Update is not an approval. Conduit
  governance remains the approval authority.
- The delivery kernel remains the authority for Plans, leases, rollout,
  rollback, and observed state.
- A localhost demonstration must not be described as a production
  deployment, an air-gap deployment, or a small-install default.
- The operational cost of self-hosting Temporal has to be stated, not
  implied away by a dev-server demo.

## Considered options

- **Temporal for the packaging run.** The Go SDK orchestrates the
  pipeline. Activities perform external I/O. Signals and Updates only
  name an approval record that an activity reads back from Conduit.
- **Extend the existing Plan lifecycle** with packaging retries, timers,
  and an approval wait. That keeps one store, and it also puts packaging
  orchestration, model retries, and proposal identity into the delivery
  kernel. Rejected: the kernel's job is Plan execution, and this would
  be a second implementation of a workflow engine inside it.
- **Implement both.** Rejected. Two durable engines for one pipeline
  will diverge, and the issue forbids it.

## Decision outcome

Temporal is the preferred durable engine for the packaging pipeline.
This record does not install it in the Hub, does not migrate Plan
execution, and does not authorize a production Temporal cluster.

The Hub exposes one read-only route, `GET /v1/packaging-runs/{runID}`.
With no worker configured, an authenticated operator receives
`configured: false`. The route has no write method. Plan status stays
on `GET /v1/plans/{planID}`. Approval stays on the existing governance
routes.

A deployment that does not supply a worker is the supported mode for a
small or disconnected install. That mode is not a reimplementation of
Temporal. The packaging run simply does not run.

### What is whose

```
pinned source
    |  Activity: assess (digest only)
    |  Activity: propose (generated file stays on the worker)
    |  Activity: validate (PASS / FAIL_REQUIRED / UNKNOWN_SECURITY / UNSUPPORTED)
    v
Temporal workflow  ----query---->  GET /v1/packaging-runs/{runID}
    |                                 packaging-run progress only
    |  Signal or Update names an approval id
    |  Activity reads the Conduit approval record and rejects
    |  anything that does not match this proposal, artifact, and target
    v
Conduit governance                approval authority
    |
    |  Activity: reconcile an existing Plan receipt, recheck execution
    |  controls, submit at most once
    v
Delivery kernel                   Plan, lease, rollout, observed state
```

Packaging-run progress is Temporal's. Approval is Conduit governance's.
Plan status is the delivery kernel's. A Temporal event cannot grant
deployment permission.

### Identity, history, and retention

A run is bound to its run id, source digest, template version, canonical
tool versions, and target-plan reference. The proposal revision and
artifact digest are known only after propose, and approval has to match
those values. Workflow history stores digests, short reasons, and the
Plan id. It does not store generated files or credentials. Reasons are
capped at 240 bytes. Continue-as-new carries that same bounded state
when history length reaches 256 events, or when the server suggests it,
and stops after four continuations.

The dev server keeps that history in the sqlite file passed to
`--db-filename` until an operator deletes the file. A production
namespace would set its own retention; this proof does not, because it
does not run a production namespace. Seven days is the retention to
set when a production namespace is actually created. It is not a
behavior of this demo.

### Activities

Every external read or write is an activity: assess, propose, validate,
read the approval record, and hand off. Each has a start-to-close
timeout and a 5 second heartbeat timeout. Transient failures retry at
most three times. `BadInput` and `ExecutionControl` do not retry.
A failed, unknown, or unsupported validation completes the run as
blocked. It is not retried into `PASS`.

Idempotency keys bind a step to the run, the revision, and the digest.
The first stored receipt for a key wins. A later proposal with a
different digest drops earlier validation and approval
(`domain.ApplyProposal`). The handoff activity reconciles the journal
before it submits, including when the first attempt wrote the journal
and then lost the acknowledgement.

### Worker evolution

The workflow records `GetVersion` marker `packaging-pipeline-v1`. A
worker that does not understand that marker fails closed. A later
behavior change needs a new version. Replay of a completed dev-server
history against the same worker is part of
`TestDevServerRestartRecoversWaitAndHandoff`.

### Local measurement

Pinned versions: Go SDK `go.temporal.io/sdk` v1.51.0, CLI v1.10.0,
embedded server 1.32.2, embedded UI 2.56.1. The CLI archive SHA-256 is
`521f461f3d108902763bb71e85cd0fd382dc9357b59604ae03da6ae734d16890`.

One recovery run on this machine, with the listener check in the test,
recorded startup 214 ms, worker RSS 144568 KiB, and a sqlite file of
704512 bytes after the recovered run. Every TCP socket owned by a
separately inspected `start-dev` process was 127.0.0.1, including
frontend, UI, and the extra local ports the dev server opens. The SDK
also passes `frontend.enableServerVersionCheck=false`, and the demo
passes `--ui-disable-news-fetch`. The test did not disconnect the
machine's network interface. That is not an air-gap result.

### Self-hosted cost

A production Temporal service is a frontend, a history service, a
matching service, workers, and a database with schema upgrades,
backups, authentication, TLS, namespace retention, and an owner. The
development server is one process with sqlite. Its own help text says
it is not for production and that it skips HTTP security checks. This
decision does not treat that process as a production topology or as a
UDS architecture.

The dev server's RSS is small enough that the binary fits on a modest
machine. That does not mean a small Conduit install should run a
Temporal cluster. The supported answer for that install is the
unconfigured route. Building a private workflow engine inside Conduit
to avoid the dependency is the option this record rejects.

Offline orchestration does not supply a model. This proof has no model
call. A packaging run that needs a model still needs whatever network
or local runtime that model requires.

### Contracts for the packaging issues

- **#341** owns assessment and proposal content. Packaging activities
  should call that contract. This proof uses a pinned fixture so the
  engine can be judged without that implementation.
- **#342** may read `GET /v1/packaging-runs/{runID}` and must keep
  assistant consent, proposal review, and deployment approval separate.
  The route is not an approval control.
- **#343** owns the validation corpus and the negative cases. The
  workflow already treats a required failure, unknown security
  evidence, and an unsupported requirement as terminal. It does not
  define the corpus.

## Consequences

- Good: packaging retries, the review wait, and crash recovery are
  Temporal's problem, with tests for the failure cases the issue lists.
- Good: the delivery kernel and governance routes are unchanged in
  authority.
- Bad: a real deployment now has an operational dependency this
  repository has not staffed. The proof does not create that service.
- Bad: the dev-server test is skipped unless `CONDUIT_TEMPORAL_CLI`
  points at the pinned binary, so CI does not repeat the crash drill.
- Unknown: production schema upgrades, backup restore, and multi-worker
  behavior were not run. They remain the cost of a later deployment
  issue, not properties of this one.

## Confirmation

`go test ./internal/packagingworkflow/...` covers the decision rules,
the fixture gates, the workflow failures, continue-as-new, and replay
inputs. `CONDUIT_TEMPORAL_CLI=... go test ./internal/packagingworkflow/pipeline/
-run TestDevServerRestartRecoversWaitAndHandoff` is the crash drill.
`scripts/packaging-temporal-demo.sh` runs that drill.

## More information

- Supersedes: none
- Superseded by: none
- Operator notes: `../operations/temporal-packaging-poc.md`
- Issues: #349, with integration notes for #341, #342, and #343
