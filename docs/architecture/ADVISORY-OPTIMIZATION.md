# Advisory optimization boundary

Platform may optimize the allocation of already-admissible work, but an
objective function is never authority. Optimization cannot approve a Change
Request, weaken recall or maintenance policy, alter immutable Product/Release
coordinates, infer ordering from an opaque version, issue a Plan, or replace
exact Spoke-reported state. Domain code must derive the eligible work and hard
constraints before calling the optimizer and recheck them at the existing last
safe point before execution.

`internal/optimization` models an allocation problem with positive work demand,
positive resource capacity, and an explicit eligibility relation. For a target
normalized makespan `T`, fractional feasibility is a maximum-flow problem:

```text
source -> work item (demand)
work item -> eligible resource (demand)
resource -> sink (T * capacity)
```

Binary search over `T` solves the convex relaxation. The fractional answer is a
lower bound because operational work is indivisible. Deterministic
largest-demand-first rounding produces an executable recommendation. Results
report the relaxation bound, incumbent and rounded makespans, relative
improvement, optimality gap, mean utilization, imbalance, fractional evidence,
and exact assignments.

Estimated monetary cost uses integer micro-units, an uppercase three-letter
currency code, a price-source identifier, and an RFC 3339 `asOf` instant. Each
indivisible item supplies its exact estimate per eligible resource; a missing
estimate remains unknown and suppresses aggregate cost instead of being treated
as free. Cost breaks ties only after the makespan objective. These are
price-model estimates, not billing facts. `MeasureCostAccuracy` admits only
complete provider-authoritative outcomes matching the recommended opaque
item/resource pairs and currency. It reports exact signed and absolute error in
micro-units plus integer parts-per-million without rewriting the original
recommendation; partial evidence fails closed.

The initial adapters are deliberately data-only:

- Mutation replay treats mutant IDs as opaque work coordinates, observed test
  duration as demand, isolated module copies as resources, and architecture or
  package restrictions as eligibility.
- Rollout simulation treats approved candidate IDs and Release coordinates as
  opaque, estimated execution duration or bounded exposure as demand, rollout
  lanes as resources, and precomputed promotion, maintenance, topology,
  dependency, and risk admission as eligibility.

The CLI reads this common JSON contract on standard input and emits the measured
recommendation:

```console
go run ./cmd/platform-optimizer < internal/optimization/testdata/mutation.json
go run ./cmd/platform-optimizer < internal/optimization/testdata/rollout.json
```

The evidence loop uses explicit commands:

```console
platform-optimizer recommend < request.json > recommendation.json
platform-optimizer evaluate-schedule < schedule-evaluation.json
platform-optimizer evaluate-cost < cost-evaluation.json
platform-optimizer plan-acquisition < acquisition-request.json
platform-optimizer assess-value < value-request.json
platform-optimizer assess-value-policies < value-policy-request.json
platform-optimizer assess-value-scenarios < value-scenario-request.json
platform-optimizer record-value < value-record-request.json
platform-optimizer evaluate-value < value-evaluation.json
platform-optimizer assess-experiment < experiment-evaluation.json
platform-optimizer record-twin-snapshot < twin-snapshot-request.json
platform-optimizer assess-dora < dora-request.json
platform-optimizer assess-evaluators < evaluator-batch.json
platform-optimizer record-evaluators < evaluator-batch.json
platform-optimizer resolve-evaluators < evaluator-resolution.json
platform-optimizer compile-backend-request < backend-input.json > backend-request.json
platform-optimizer solve-backend < backend-request.json > solver-receipt.json
platform-optimizer verify-backend-receipt < request-and-receipt.json
```

`scripts/run-evaluator-batch.sh` creates one sanitized snapshot receipt,
materializes that immutable tree in an ephemeral isolated workspace, and gives
each read-only model the exact same generated prompt and readable files. Only
the execution workspace is temporary; raw receipts, timing, prompt, and snapshot
identity must be written to a durable repository evidence directory. The runner
requires `CURSOR_API_KEY` in its environment and never persists it.
`scripts/normalize-evaluator-receipts.mjs` converts the raw receipts into the
content-addressed Platform batch contract. `scripts/export-zenith-evaluator-trial.mjs`
then binds the same prompt, snapshot, receipts, independent canonical dispositions,
usage, latency, and cost status into Zenith Protocol's provider-neutral format.
Its `--check` mode proves a committed export still matches a fresh derivation
without rewriting evidence.

A request pins a state snapshot, objective priority, and the source, revision,
and observation time of every compiled hard constraint. The recommendation ID
is a Go-JSON SHA-256 commitment to the canonicalized request sets and its result,
excluding only the advisory creation timestamp. It is deterministic within the
pinned Go contract, not yet a language-independent JCS claim. Schedule outcomes must
cover the exact recommended item/resource pairs and measure elapsed wall time,
including queueing. Cost outcomes follow the stricter integer-micro-unit rules
above.

Cost breaks an equal-load assignment tie only when both candidate estimates are
known under the same pinned cost model. An unknown estimate is neither free nor
worst-case and is never ordered against a known estimate; deterministic non-cost
tie breakers decide instead, and aggregate cost remains unavailable if the
selected assignment lacks an estimate.

The low-level `Solve` API enables that known-cost tie break whenever a cost model
is supplied. A content-addressed `Recommendation` enables it only when the
request explicitly declares `minimize_estimated_cost` as a secondary objective;
without that objective, price evidence is retained but does not choose the
assignment.

State acquisition is a bounded exact weighted-set-cover problem over six
non-interchangeable planes: desired, admitted, executing, observed, billed, and
experienced. A call covers a requirement only when plane, fidelity, and freshness
match. At most 63 requirements and 20 calls are accepted; incomplete coverage is
reported as UNKNOWN and is never labelled optimal. This chooses the least-cost
sufficient observation set, not the APIs with the largest payloads.

The bitemporal twin stores content-addressed facts and snapshots in a caller-owned
durable directory. `validAt` asks what held in reality; `knownAt` asks what had
been recorded by that time. Facts bind entity/attribute, state plane, unit,
fidelity (`INFERRED`, `ESTIMATED`, `SAMPLED`, `AGGREGATED`, or `EXACT`), valid
and transaction times, source-call revision, request/response digests,
and collector identity. Snapshot fields remain `UNKNOWN`, `STALE`, or
`CONFLICTING` when the evidence cannot support `KNOWN`; a requirement explicitly
declared inapplicable is `NOT_APPLICABLE`. Snapshots state whether every required
field is known or not applicable, and only a complete content-addressed `snap_`
snapshot can be promoted into a recommendation request. The file store uses
mode-0600 immutable records and synchronized directory entries; a Codespace user
should select a tracked repository evidence directory or another persistent
volume, never `/tmp`.

Value engineering preserves stakeholder and user segment, required function and
criticality, measure, unit, cardinal/ordinal scale, baseline, target, horizon,
distribution/bounds, provenance, and hard thresholds. Every alternative records
time-to-value, discount assumptions, and exact integer-micro-unit lifecycle cost
across development, infrastructure, operations, support, risk, migration, and
retirement with a pinned price basis. Conservative dominance
produces a content-addressable Pareto assessment; missing evidence and infeasible
alternatives remain separate. No ungrounded scalar score silently chooses among
non-dominated alternatives. Complete realized stakeholder outcomes join back to
the assessment and preserve exact prediction error without rewriting the frontier.
Optional stakeholder policies must explicitly weight every criterion with exact
weights totaling one; baseline-to-target normalization prevents unlike units from
being added directly, and the result reports when policy weights change the
selected Pareto alternative. Provenance-pinned scenarios independently recompute
the conservative frontier and report when explicit assumption changes alter it;
they preserve the same alternative identities so inventory changes cannot
masquerade as sensitivity. Hard constraints remove infeasible alternatives
before policy scoring or scenario comparison.

Preregistered A/B experiments turn selected uncertainties into causal outcome
evidence. The plan pins variants, assignment and randomization units, allocation,
one primary metric, non-inferiority margins for guardrails, sample and duration
bounds, analysis procedure, and stopping rule. An
outcome records exposure cost and exact effect bounds. A result is eligible for
review only when sample, primary, and every guardrail pass; it still cannot
authorize rollout. Fixed-horizon or sequential statistical correctness belongs
to the pinned analysis implementation and must be independently validated.

Independent model evaluators are read-only evidence sources, never approvers.
Every run binds provider/model/version, an exact prompt digest, the same
content-addressed input snapshot, a secret-scan contract, start/finish time,
immutable receipt identity, exact input/output/cache token usage, and price
provenance. Monetary cost is either `MEASURED` with amount and currency or
`UNAVAILABLE` with an explicit reason; an unknown charge is never represented as
free. Findings enter only as hypotheses. Model-supplied cluster keys preserve raw
claims; only an independent disposition assigns the canonical cross-model cluster
used for deduplication and calibration. Correlated wording is not independent
confirmation. A disposition needs repository provenance, and a verified defect
needs an executable gate. Each run declares complete or incomplete input access
and retains residual unknowns; incomplete access makes recall unavailable rather
than zero. Batch metrics retain latency, usage, cost status, false discovery rate,
run-level unique verified-cluster yield, precision, and recall against the
explicitly bounded verified-cluster universe. Absent dispositions remain unknown
rather than false. Records persist immutably in a caller-owned durable directory
and cannot approve or mutate Platform delivery state.

DORA software-delivery performance is an outcome subsystem, not the platform's
utility function. `AssessDORA` computes the current five measures for one named
application/service and half-open time window: deployment frequency, the full
change-lead-time distribution, failed-deployment recovery-time distribution,
change fail rate, and deployment rework rate. Durations and rates remain exact
rationals, every deployment carries source revision, and a failed deployment
without recovery evidence makes the assessment incomplete. Compare the same
service with itself over time; do not blend unlike services, rank teams, infer
user value, or optimize a single DORA number. DORA measures should be paired with
product outcomes, reliability/SLOs, security, cost, accessibility, and team
well-being, and experimental evidence is required for causal claims.

This allocation kernel is one component of the larger decision model. Further
adapters may compile temporal dependencies, network flow, storage locality,
user demand, failure domains, staffing, and risk budgets into bounded problems.
When indivisible or temporal decisions make a model non-convex, Platform must
label the mixed-integer or heuristic stage and retain the convex relaxation as
a measurable lower bound; it must not call a rounded recommendation optimal.

No automatic Plan integration is present. Adoption requires observed-vs-
predicted outcome recording and proof that every domain constraint remains
fail-closed under stale input and concurrent state changes.

The phased solver, Kubernetes-provider, calibration, and distribution plan is
defined in [OPTIMIZER-TOOLCHAIN-PLAN.md](OPTIMIZER-TOOLCHAIN-PLAN.md). It keeps
the native kernel as the conformance oracle and requires independently verified
receipts before HiGHS, CP-SAT, or any provider recommendation can influence a
governed proposal.
