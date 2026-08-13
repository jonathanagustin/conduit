# Optimizer toolchain adoption plan

Status: **Phase 0 implemented; external backends and provider adapters remain
planned**.

This plan extends the [advisory optimization boundary](ADVISORY-OPTIMIZATION.md)
without moving approval, leasing, or execution authority into an optimizer. A
solver answers a bounded mathematical question over an immutable snapshot. It
does not decide whether the underlying change is safe or authorized.

## Architectural shape

```text
read-only providers -> bitemporal snapshot -> model compiler
                                               |
                          native / HiGHS / CP-SAT backends
                                               |
                         recommendation + solver receipt
                                               |
                      independent verification and outcomes
                                               |
                  governed proposal and live admission recheck
```

The native deterministic max-flow, exact set-cover, conservative Pareto, and
exact-rational implementations remain the reference and disconnected fallback.
External backends are optional, digest-pinned tools. Kubernetes controllers are
read-only evidence sources from Platform's perspective; Platform does not
become a second autoscaler, scheduler, or node provisioner.

## Common backend contract

No second solver is admitted until a provider-neutral contract records:

- exact input snapshot, model, hard-constraint, and objective-priority digests;
- backend name, semantic version, executable or image digest, architecture,
  deterministic parameters, seed, worker count, and resource/time limits;
- `OPTIMAL`, `FEASIBLE`, `INFEASIBLE`, `TIME_LIMIT`, `ERROR`, or `UNKNOWN`
  termination without translating timeout or absence into infeasibility;
- incumbent objective, independently meaningful lower bound, and optimality
  gap using exact units rather than formatted floating-point text;
- complete assignments and a bounded, redacted infeasibility explanation when
  the backend provides one;
- start/finish time and content-addressed receipt identity; and
- independent verification that every returned assignment satisfies the
  compiler-owned hard constraints.

Backend selection is capability-based and explicit. An unavailable preferred
backend may fall back only when the request authorizes a named fallback and the
receipt records it. Conflicting feasible answers remain disagreement evidence;
the system does not silently choose the cheapest answer.

## Adoption sequence

### Phase 0: backend and conformance boundary

Define the request/receipt interface and wrap the current native solver. Build a
corpus containing feasible, infeasible, exact-boundary, overflow, timeout,
architecture, failure-domain, maintenance, PDB, and disk-pressure cases. The
native backend must reproduce current recommendations byte-for-byte. Mutation
tests must prove that dropping a hard constraint, changing a bound, or claiming
optimality from a feasible result fails.

Exit gate: a versioned native receipt replays from an immutable snapshot, and
the optimizer still has zero imports or routes capable of issuing, approving,
leasing, or finishing a Plan.

Implemented evidence:

- `SolverRequest` binds the canonical recommendation, snapshot, model,
  hard-constraint and objective digests, capability, ordered backend policy,
  seed, worker count, and bounded time/memory limits;
- `SolverReceipt` binds the exact native executable digest and architecture,
  backend/version, attempted and selected backend, explicit fallback,
  termination, exact objective strings, result, redacted explanation, and
  ordered runtime timestamps by content ID;
- the generic verifier independently checks every item exactly once, hard
  eligibility, demand, relaxation feasibility, the native lower-bound oracle,
  resource loads/utilization, exact known-cost aggregation, and optimality;
- `OPTIMAL`, `FEASIBLE`, `INFEASIBLE`, `TIME_LIMIT`, `ERROR`, and `UNKNOWN`
  remain distinct, and native execution cannot claim secondary-objective
  optimality;
- `platform-optimizer compile-backend-request`, `solve-backend`, and
  `verify-backend-receipt` provide the internal evidence workflow; and
- `TestOptimizerHasNoDeliveryAuthorityImports` protects the package/command
  boundary from control-plane, Agent, or Conduit authority imports.

Executable gate:

```text
go test -race ./internal/optimization ./cmd/platform-optimizer -run 'Solver|Backend|OptimizerHasNoDeliveryAuthorityImports'
```

### Phase 1: HiGHS linear and mixed-integer backend

Use [HiGHS](https://highs.dev/) for LP/MIP allocation, capacity, and lifecycle
cost models. Run it as a resource-bounded, networkless, digest-pinned external
tool rather than adding C/C++ runtime state to the Hub. Preserve the native
fractional relaxation as an independent lower-bound oracle where the models
overlap.

Exit gate: the pinned backend passes the corpus on amd64 and arm64, every
feasible result passes the independent constraint verifier, infeasible and
timeout outcomes retain their distinct states, and shadow outcomes record
runtime, gap, disagreement, and realized error without changing a Plan.

### Phase 2: OR-Tools CP-SAT discrete backend

Use [OR-Tools CP-SAT](https://developers.google.com/optimization/cp) for
indivisible placement, rollout ordering, topology, maintenance windows,
dependency sequencing, and bounded disruption. Integer scaling and rounding
rules are part of the model revision. Deterministic execution parameters are
recorded; a time-limited incumbent is `FEASIBLE`, never `OPTIMAL`.

Exit gate: CP-SAT kills targeted architecture, dependency, topology, PDB, and
time-window mutations; its assignments pass the same independent verifier; the
relaxation bound remains visible; and fallback behavior is tested with the
backend absent.

### Phase 3: read-only provider adapters

Provider adapters compile facts into the existing bitemporal twin:

| Source | Facts admitted | Authority deliberately excluded |
| --- | --- | --- |
| Kubernetes scheduler APIs | predicates, requests, allocatable resources, topology and placement failures | binding or scheduler replacement |
| VPA recommendation | bounded CPU/memory recommendation and observation window | automatic request mutation |
| HPA/KEDA | replica demand and event backlog | direct replica control |
| Karpenter/Cluster Autoscaler | eligible capacity, instance constraints, consolidation/provisioning observations | node provisioning |
| OpenCost and provider billing | allocation, idle/shared cost, storage, network, and authoritative billed outcomes | treating estimates as invoices |
| Cilium/Hubble | policy realization and bounded flow telemetry | network-policy ownership outside H73 |
| Kueue/Volcano | queue, quota, gang and batch feasibility when installed | installing or replacing batch schedulers |
| Prometheus/OpenTelemetry | demand, latency, saturation, error and SLO observations | inferring missing signals as healthy |
| Kepler | energy estimates with explicit model provenance | treating modeled energy as metered billing |

Every adapter is optional, read-only, freshness-bounded, and bound to exact
request/response digests, collector identity, source revision, and observation
time. Platform consumes controllers already selected by the environment owner;
it does not install all of them as a mandatory stack.

Exit gate: each admitted provider has fixtures for missing, stale, conflicting,
zero, and not-applicable evidence and at least one sanitized live capture before
its facts may influence a recommendation.

### Phase 4: governed proposal integration

Translate a recommendation into a new proposal only through an explicit
compiler that preserves its snapshot and solver receipt. Normal Change Request
policy evaluates the proposed diff. Capacity, policy, recall, maintenance,
health, cost-owner, and ownership evidence is reacquired at approval and again
at the last safe point before lease. Drift makes the proposal non-leaseable; it
does not trigger automatic re-optimization under stale authority.

Exit gate: an end-to-end shadow rollout proves recommendation -> proposal ->
approval -> revalidation -> execution -> outcome linkage, and adversarial tests
prove the optimizer cannot bypass any existing governance path.

### Phase 5: calibration and lifecycle

Promote a backend or provider from shadow use only after a bounded workload
records feasibility disagreement, optimality gap, solve latency, acquisition
cost, predicted-versus-realized schedule/cost/value error, and operational
failures. Preregistered experiments remain subject to the existing guardrail and
non-inferiority contract. A backend has an explicit disable path and its old
receipts remain replayable after retirement.

Exit gate: the P9 process trial meets its measured criterion. A solver that is
slower, less reliable, or no more decision-useful than the native reference is
removed rather than retained for feature count.

## Tool disposition

- **Adopt:** native reference backend, HiGHS, CP-SAT, scheduler feasibility,
  VPA recommendations, OpenCost, provider billing, and existing Cilium/Hubble
  evidence.
- **Conditional:** KEDA, Karpenter/Cluster Autoscaler, Kueue/Volcano,
  Descheduler, Kepler, Argo Rollouts, and Flagger only where an environment
  already has the corresponding workload or controller contract.
- **Backend option, not baseline:** SCIP or a commercial solver behind the same
  receipt and license boundary when the open backends cannot solve a measured
  model adequately.
- **Reject:** embedding credentials in solver inputs, installing competing
  controllers automatically, accepting an unverified assignment, scalarizing
  hard constraints, or granting any solver direct Plan mutation authority.

## Delivery and distribution

The native backend ships in `platform-optimizer`. External backends ship as
separately versioned, signed, SBOM-bearing multi-architecture OCI artifacts and
optional air-gap bundle components. Product and Environment configuration stay
outside this repository. A disconnected deployment can always run the native
backend; absence of an optional backend is explicit capability state, not a
reason to download code at runtime.

`platform-optimizer` is an internal evidence executable. `conduit` remains the
single supported operator CLI and may consume or display receipts through
governed APIs; this plan does not introduce a second public CLI surface.
