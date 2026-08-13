# Platform system model and agentic operating model

Status: **architecture decision and dependency-ordered adoption plan**. This
document consolidates the 2026-08-12 Platform, Foundry-recovery, optimizer,
workspace-bootstrap, telemetry, and agentic-systems investigation. It does not
admit a solver or provider, authorize tenant access, issue a Plan, change cloud
resources, or treat an estimate as a bill.

The recommended shape is a domain-owned Hub/Spoke control plane with hexagonal
ports and adapters, immutable evidence, and independently governed execution.
Adopt open protocols at interoperability boundaries, but build the small
Platform-specific authority and state-fidelity kernel: no existing framework
combines tenant-safe delivery governance, a six-plane bitemporal state twin,
proof-carrying optimization, and last-safe-point revalidation.

## Decisions

1. Keep **domain authority in Platform**, not in an agent framework, solver,
   telemetry system, Kubernetes controller, or MCP server.
2. Treat Hub/Spoke as deployment topology and trust boundaries. Do not use it
   as a substitute for bounded contexts.
3. Make decisions, observations, solver inputs, proposals, approvals,
   revalidations, and outcomes immutable and content-addressed. Mutable views
   are rebuildable projections; secrets, leases, and ephemeral health are
   references or observations, not embedded snapshot payloads.
4. Keep desired, admitted, executing, observed, billed, and experienced state
   separate. Missing or stale facts are `UNKNOWN` or `STALE`, never optimistic
   defaults.
5. Use optimization as an advisory decision service. A feasible or optimal
   answer cannot approve itself; governance still approves, and a fresh
   last-safe-point acquisition still controls leaseability.
6. Use Go for the existing Platform domain kernel and ports. Use Rust for the
   narrowly bounded native CP-SAT protocol adapter and other new memory-safety-
   sensitive collectors only when it improves a measured boundary. A wholesale
   language rewrite would add migration risk without increasing state fidelity.
7. Let Factory own workspace boot and cache offload. Let Platform own exact
   tool versions, checksums, and its installer. Never offload source, Git data,
   credentials, state-twin facts, or evidence receipts.

## Domain-driven bounded contexts

The current `internal/controlplane` package contains several real domains. Move
them behind explicit contracts incrementally; do not perform a flag-day tree
rewrite. Each context owns its vocabulary, invariants, repository port, events,
and authorization checks. Cross-context calls use typed application services or
immutable events rather than shared mutable records.

| Bounded context | Owns | Must not own |
| --- | --- | --- |
| Catalog and release | Product, Release, Module, channel, immutable artifact coordinate, dependency graph, retirement | environment credentials, rollout execution |
| Environment topology | Environment identity, Hub/Spoke ownership, profile, federation, Entity placement | product publication, scheduler decisions |
| Governance and admission | Change Request, approval policy, grant, suppression, recall, admission evidence | artifact mutation or task execution |
| Planning and execution | immutable Plan, step DAG, lease/fence, heartbeat, cancellation, terminal execution receipt | approval policy or optimizer selection |
| Fleet and health | installation target, reported state, rollout/lifecycle projection, health target and observation | desired-state mutation or inferred billing |
| Artifact publication | build/export/publication lineage, signature, SBOM, scan and promotion evidence | tenant secrets or release approval |
| Advisory optimization | acquisition manifest, temporal facts, snapshot, model, solver receipt, proposal, revalidation, calibration, outcome | Plan approval, lease issuance, controller writes |
| Identity and audit | session principal, tenant scope, authorization decision, append-only audit category | business-domain policy definitions |
| Foundry interoperation | anti-corruption translations for recovered/public Foundry contracts | Platform domain objects as unvalidated wire shapes |
| Spoke execution | environment-owned identity, task adapters, status reporting | Hub policy or long-lived external credentials |

An idiomatic target package shape is:

```text
internal/<context>/
  domain/          entities, values, invariants, domain events
  application/     commands, queries, use cases, port interfaces
  adapters/        HTTP, file, Kubernetes, provider and wire implementations
  testing/         contract fixtures and adversarial port suites
cmd/<binary>/      composition root only
```

This is a direction, not a mechanical naming mandate. Start with ownership and
dependency tests, then move code. Domain and application packages must not
import HTTP routers, Kubernetes clients, filesystem implementations, solver
processes, telemetry exporters, `internal/agent`, or `internal/conduitcli`.
Adapters may depend inward. A command is the composition root.

### Hub/Spoke application

The Hub owns global desired state, governance, immutable Plans, leases, and the
fleet projection. A Spoke owns only environment-local execution and observation
under an externally provisioned identity. The Hub sends a fenced, digest-bound
task lease; the Spoke returns heartbeats and immutable execution evidence. A
Spoke cannot widen its capability, approve work, or rewrite a Plan. The Hub
does not impersonate the Spoke or copy its durable credentials.

Every cross-boundary message binds tenant, environment, Plan, step, attempt,
lease, fence, artifact coordinate, schema version, source revision, valid time,
known time, and content digest as applicable. Retries are idempotent. A stale
fence, foreign tenant, changed artifact coordinate, or unknown schema fails
closed.

## Complete state is six state planes, not one status API

Whole-system fidelity needs six non-interchangeable planes:

| Plane | Question | Primary authorities |
| --- | --- | --- |
| Desired | What should exist? | Platform catalog/environment/profile/installations, Git/Flux desired resources, budgets, SLOs, policy objectives |
| Admitted | What was permitted, and against which evidence? | validation, artifact scan/signature/SBOM, policy/grant/approval, capacity/security/cost constraints |
| Executing | What work is actually in flight? | Plan lease/heartbeat/task timeline, Kubernetes workloads/jobs, scheduler/build/data transaction state |
| Observed | What exists and how is it behaving? | Platform reported state/fleet, Kubernetes status/events, provider APIs, Prometheus/OTel, Cilium realization and Hubble flows |
| Billed | What did it actually cost? | provider invoice/export and adjustments; OpenCost allocation is a reconciled estimate unless invoice-backed |
| Experienced | What outcome did a stakeholder experience? | privacy-safe journeys, SLOs, support/feedback, adoption/retention and attributable business outcomes |

Each fact has an authority, entity and attribute vocabulary, unit, fidelity,
`validAt`, `knownAt`, observation window, freshness limit, source revision,
request/response digests, collector identity, and deletion/tombstone semantics.
Facts from different authorities coexist. Desired state cannot overwrite
observed state, a metric cannot overwrite a bill, and UI telemetry cannot be
relabeled as user value.

The recovered Foundry route inventory is broad but not equivalent to runtime
knowledge: the current scorecard contains 5,481 operations across 117 mounts,
while 816 are verified-200, 36 are request-typed, and 4,629 are bundle-path-only.
A route or recovered schema proves neither authorization, deployed behavior,
freshness, nor that an event occurred.

### Highest-fidelity read portfolio

Do not build a giant `status` endpoint. Compile a requirement manifest into the
lowest-cost set of authority reads, then bind every raw response to normalized
facts. Prefer bulk/list/watch calls with pinned revisions and bounded cursors;
drill down only when a requirement remains unresolved.

1. Platform desired/governance snapshot: Products, Releases, Modules,
   Environments, profiles, entities/installations, policies, Change Requests,
   Plans and approvals.
2. Platform reported/fleet snapshot: reported state, installations, health
   targets and bounded health observations.
3. Kubernetes and Flux list/watch at a pinned `resourceVersion`: workloads,
   nodes, jobs, events and reconciliation conditions.
4. Prometheus range queries and OpenTelemetry traces over the exact same
   observation window; distinguish missing, zero, stale, and conflicting data.
5. Cilium policy realization and bounded Hubble flow evidence as separate
   sources; configuration alone does not prove enforcement.
6. Foundry logical execution/data reads for scheduler, job/build, catalog,
   object and transaction state, selected by discovered resource type and only
   when behavior is admitted.
7. Usage aggregation, OpenCost allocation, and provider invoice/export. Retain
   disagreement and allocation residual rather than selecting a convenient
   number.
8. Identity, authorization and append-only audit snapshots, with tenant scope
   and privacy-safe evidence.
9. Experience batch: journey/SLO telemetry, feedback/support, and business
   outcome/revenue facts joined only by consented, privacy-safe identifiers and
   preregistered causal contracts.

The acquisition compiler is a dependency DAG: discovery, page/watch, detail,
extract, reconcile, snapshot. The existing exact weighted set cover remains a
useful bounded kernel, but a whole-system compiler must also represent batching,
pagination, conditional reads, setup cost, rate limits, source failure risk,
freshness scheduling and correlated-source redundancy. Every required field is
either covered by a validated extractor or explicitly `UNKNOWN`; prose-only
gaps do not count toward completeness.

## Hexagonal optimization boundary

Optimization is one bounded context with these inward ports:

- `StateRequirementRegistry`: versioned field, authority, fidelity, freshness,
  scope, unit, join key and privacy policy.
- `AcquisitionReadPort` and `AcquisitionFactExtractor`: read-only transport and
  schema-specific normalization, separately testable.
- `TemporalFactSink` and `TwinSnapshotStore`: append-only facts and complete,
  content-addressed snapshots.
- `ModelCompiler`: converts one immutable snapshot and explicit policy into a
  versioned mathematical model.
- `SolverBackend`: a bounded request/receipt protocol; solver output is
  untrusted until compiler-owned independent verification passes.
- `ProposalEvidenceStore`, `RevalidationPort`, `CalibrationStore`, and
  `OutcomeStore`: immutable lifecycle evidence.

Outbound adapters include Platform Hub fixtures, bounded HTTP dispatch,
Kubernetes, Prometheus, OpenCost, provider billing, Cilium/Hubble, solver
processes and file/OCI evidence stores. They do not leak SDK types into the
domain. Credentials remain injected by the deployment environment and are
never accepted in an optimizer request or persisted receipt.

## Optimization portfolio

There is no single “king” algorithm. Use the most restrictive model family that
faithfully represents the decision and retain an independently checkable
baseline.

| Decision shape | Method | Current or candidate technology | Required evidence |
| --- | --- | --- | --- |
| minimum authority reads | exact weighted set cover | native exact branch-and-bound | coverage, freshness, fidelity, call cost and explicit unknowns |
| indivisible placement, ordering, maintenance windows | constraint/integer optimization | source-built OR-Tools 9.15 CP-SAT behind the Rust adapter | feasibility replay, exact objective prefix/bounds, canonical tie fixing |
| allocation, capacity and lifecycle cost | LP/MIP | HiGHS 1.15.1 unadmitted external shim | exact compiler mapping, lower bound/gap, cross-architecture receipts |
| smooth rightsizing and risk tradeoffs | convex QP/SOCP/conic optimization | evaluate source-pinned Clarabel.rs behind the same backend protocol | convexity/domain proof, tolerances, residuals, dual certificate where available |
| uncertainty in demand/prices/failures | robust or stochastic optimization | scenario compiler over native/HiGHS/CP-SAT; no new authority | scenario provenance, coverage probability, regret/CVaR and sensitivity |
| several incompatible objectives | constrained lexicographic and Pareto analysis | native conservative Pareto plus solver phases | hard constraints first; cost/value/risk remain separate with sensitivity |
| queues, rollout congestion and retries | queueing theory and discrete-event simulation | versioned simulator, then optimizer | calibrated distributions, confidence interval and holdout error |
| continuous feedback/autoshutoff | model-predictive/control policy plus safety state machine | advisory controller feeding governed Plans | hysteresis, minimum-on/off time, cooldown, SLO/capacity guard, rollback |
| future observations worth buying | value-of-information/risk-aware acquisition | dependency-DAG compiler plus bounded exact selection | expected uncertainty reduction, critical redundancy, latency and monetary cost |

The canonical objective is constrained and lexicographic, not one opaque score:

```text
subject to tenant isolation, policy, safety, capacity, SLO, disruption,
           freshness and confidence constraints
1. minimize hard violations (must be zero)
2. minimize service risk and worst-case regret
3. maximize verified stakeholder value / gross profit contribution
4. minimize total lifecycle cost and time-to-value
5. minimize operational complexity and acquisition/compute cost
```

Report the Pareto frontier and sensitivity when objectives conflict. Revenue,
profit, engineering effort, support burden, risk, latency, carbon and user value
need explicit units and provenance; do not collapse them into a fabricated
cardinal utility without an approved policy.

### Cost model and autoshutoff

The minimum credible total-cost calculation is:

```text
provider invoice + Kubernetes allocation residual + storage + backup + IOPS
+ network/egress + load balancers + control-plane fees + observability
+ licenses/support + engineering/on-call labor + change/risk cost
- discounts - credits - attributable avoided cost
```

OpenCost is useful for vendor-neutral Kubernetes allocation and exposes
resolution-dependent error; it is not automatically an invoice. Provider
billing, discounts, tax, commitments, currency and invoice revisions remain
separate billed facts.

Autoshutoff should target an admitted capability, not “anything idle.” Eligible
resources require an owner, schedule/idle signal, restart and data-durability
contract, dependency graph, minimum availability, SLO guard, maintenance
window, cost rate and rollback path. Databases and stateful systems default to
ineligible. A stop recommendation needs hysteresis and cooldown, is approved as
a normal Change Request/Plan, and is revalidated at the last safe point.

RDS means Amazon Relational Database Service. It is managed, stateful database
infrastructure whose cost can include instance, storage, I/O, backups, replicas,
data transfer and support. Platform should model it only after an authorized
provider inventory and billing adapter supplies exact resource and invoice
facts. It must not infer an RDS resource from a Kubernetes name or shut one down
from utilization alone.

## Agentic task execution

The durable unit is an immutable `WorkSpec`, not a chat turn. It binds goal,
inputs, authority, capabilities, owned scope, dependencies, budgets, gates,
expected outputs, rollback and stop conditions. A lease carries attempt and
fence identities. A `WorkReceipt` binds the exact spec, inputs, outputs, tool
calls, exercised capabilities, gates, unknowns and independent verifier. The
author cannot certify its own high-impact work.

The idiomatic execution loop is:

```text
goal -> decompose dependency DAG -> lease fenced work item
     -> acquire immutable inputs -> act within narrowed capability
     -> run local gates -> emit receipt -> independent verification
     -> integrate -> observe outcome -> calibrate -> next item or stop
```

Independent tasks may run concurrently. Shared files and authority domains use
single-writer ownership. Retries are bounded and idempotent. A task stops when
its evidence-based acceptance criteria pass, when its budget expires with an
explicit partial result, or when a named missing authority remains unresolved.
“More calls” and “fewer calls” are not success metrics; benchmark task success,
completeness, required evidence, latency, tokens, monetary cost, calls and
retries on representative evals.

### Adopt, trial, or build

| Boundary | Decision | Rationale |
| --- | --- | --- |
| OpenAPI/JSON Schema/protobuf | adopt | versioned transport and generated contract validation |
| MCP specification `2026-07-28` | adopt at agent-to-tool/context edge | stateless self-describing requests, authorization routing, cacheable catalogs and extensions; not durable work authority |
| A2A 1.0.x | trial only for independent Hub/organization agent federation | discovery and opaque-agent task interoperability; unnecessary inside one authority kernel |
| OpenTelemetry | adopt base traces/metrics/logs | vendor-neutral correlation; pin GenAI semantic mappings while agent spans remain developmental |
| Responses API programmatic tool calling | evaluate for bounded read/filter/join stages | useful only when representative evals retain completeness and evidence; keep approval and semantic judgment direct |
| Temporal or LangGraph-style durable execution | seam evaluation | durable replay/checkpointing is valuable, but must not create a second lease/fence/Plan authority |
| CloudEvents/SPIFFE/OIDC/OCI/SLSA/Sigstore | adopt at their existing boundaries | event envelope, workload identity and immutable supply-chain evidence |
| Platform WorkSpec/WorkReceipt, six-plane twin, authority/revalidation and outcomes | build | these are product-specific semantics no interoperability protocol supplies |

Current official references: [OpenAI model and agentic tool guidance](https://developers.openai.com/api/docs/guides/latest-model),
[MCP 2026-07-28](https://blog.modelcontextprotocol.io/posts/2026-07-28/),
[A2A specification](https://github.com/a2aproject/A2A/blob/main/docs/specification.md),
[OpenTelemetry GenAI agent spans](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-agent-spans.md),
[Temporal durable execution](https://docs.temporal.io/),
[Clarabel.rs](https://clarabel.org/stable/), and
[OpenCost specification](https://opencost.io/docs/specification/).

## Telemetry and value measurement

Telemetry is an evidence adapter, never the source of desired state or approval.
Trace IDs join Plan, lease, task, workload, provider request and user journey;
stable tenant/environment/product/release/resource/cohort IDs supply the domain
join. High-cardinality identifiers remain access-controlled and are not metric
labels. Raw prompts, response bodies, tenant data, audit detail, user content
and secrets are off by default; record hashes, bounded attributes and typed
error categories.

Required signals include acquisition freshness and errors; solver queue/solve/
verify time, termination, bound and gap; proposal/revalidation drift; lease and
fence failures; rollout SLOs; cost-estimate/invoice residual; user-journey
availability; realized value and prediction error. Every KPI defines numerator,
denominator, population, window, unit, source, owner, lag, privacy class and
failure behavior. `0`, missing, stale, conflicting and not-applicable are
different values.

## Dependency-ordered implementation plan

1. **Freeze context ownership.** Publish the context map, allowed dependency
   graph and import tests. Name aggregate and repository owners before moving
   files.
2. **Compile the state manifest.** Enumerate required fields in all six planes;
   bind each to an authority/extractor/freshness policy or explicit unknown.
   `internal/optimization/state_manifest.go` is the current typed manifest;
   its `UNKNOWN` entries remain visible until an authorized provider contract
   and sanitized capture replace them.
3. **Complete evidence adapters.** Admit bulk Platform, Kubernetes/Flux,
   Prometheus/OTel, network, provider billing and experience adapters one at a
   time with sanitized fixtures. Read-only capabilities only.
4. **Finish solver admission.** Complete CP-SAT amd64/arm64 native, closure,
   mutation, deterministic semantic and signed supply-chain evidence; do the
   remaining HiGHS native matrix. Evaluate Clarabel only after a convex use case
   and corpus exist.
5. **Govern proposals.** Require complete authoritative acquisition receipts at
   approval and lease; replay the exact compiler-owned transformation; preserve
   author/verifier separation.
6. **Measure outcomes.** Join executing/observed/billed/experienced facts to the
   approved proposal and record disagreement, forecast error and realized value.
7. **Run shadow and experiments.** Keep candidates non-influencing until
   capability lifecycle gates pass; preregister objective, sample, duration,
   stopping and rollback.
8. **Extract bounded contexts incrementally.** Move one vertical slice with
   contract tests and adapters at a time. Preserve HTTP and stored-data
   compatibility through anti-corruption adapters.
9. **Introduce autoshutoff last.** Start with recommendations and dry-run savings;
   admit only resource classes with complete restart/durability/SLO evidence.
10. **Calibrate continuously.** Version thresholds and compare forecast to
    realized cost, SLO and value; retire capabilities whose evidence regresses.

## Hypotheses and falsification gates

- **State-manifest completeness:** a generated field-to-authority manifest will
  expose materially more unknown state than the current small registry. Fail CI
  on an unmapped required field.
- **Adaptive acquisition:** discovery/page/detail/watch compilation will reduce
  real request cost and stale template calls. Compare complete receipts, bytes,
  latency and provider throttling on a fixed corpus.
- **Cross-plane receipt:** a privacy-safe content receipt can bind Plan ID,
  Kubernetes revision, telemetry window, invoice revision and outcome cohort
  without copying raw tenant data. Reject any unbound join.
- **Billing triangulation:** provider invoice plus OpenCost and usage estimates
  will make allocation residual more useful than declaring one source correct.
  Track residual by category and retain unknown currency/discount components.
- **Experience causality:** journey telemetry alone will not predict stakeholder
  value reliably. Require preregistered outcome and experiment contracts.
- **Risk-aware acquisition:** failure/freshness penalties and critical-source
  redundancy will outperform scalar least-call selection on decision error at a
  bounded cost increase.
- **Rust boundary:** the source-built Rust/native solver adapter can shrink and
  harden the CP-SAT runtime closure without changing Go domain semantics. Gate
  on cross-architecture receipts, CVE/license closure, memory bounds and adapter
  mutation tests rather than language preference.
- **Agentic receipts:** immutable WorkSpec/WorkReceipt plus lease/fence binding
  will lower duplicate work, scope conflicts and unverifiable completion. Track
  retries, conflicts, rejected receipts and independent-verification defects.
- **Bootstrap ownership:** Factory can restore reproducible Platform caches and
  the pinned toolchain after `/tmp` loss without mutating source or durable
  evidence. Test boot twice and a cache-target reconstruction fixture.

## Explicit unknowns and non-goals

- Most recovered Foundry routes still lack deployed behavior admission and
  authorized tenant evidence.
- Provider billing/invoice, complete identity-to-cost joins, network enforcement
  realization, experience/business-outcome sources, and cross-plane correlation
  remain incomplete until authorized adapters are admitted.
- No document or optimizer result authorizes live tenant reads, deployment,
  resource shutdown, provider spending, or other external mutation.
- No plan proposes rewriting all Platform or recovered Foundry code in Rust.
- No framework selection changes the Hub's approval, lease/fence, tenant, audit,
  secret, or immutable-artifact invariants.
