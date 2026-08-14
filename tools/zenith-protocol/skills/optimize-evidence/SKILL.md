---
name: optimize-evidence
description: Model and improve a real system through provenance-bound state acquisition, bitemporal snapshots, constrained optimization, value engineering, Pareto analysis, independent evaluators, and predicted-versus-realized calibration. Use when optimizing delivery, operations, cost, capacity, scheduling, networks, user outcomes, development workflows, or any multi-objective system where missing state, uncertainty, time, and hard safety or governance boundaries matter.
---

# Optimize evidence

Build a decision-focused digital twin. Optimize only from immutable evidence,
keep every gap explicit, and measure whether recommendations work.

## Run the loop

1. **Name the decision.** State the controllable variables, time horizon,
   stakeholders, required functions, authority boundary, and action that will
   consume the result. Do not model the world without a decision.
2. **Declare required state.** For every field, name its desired, admitted,
   executing, observed, billed, or user-experienced plane; authoritative source;
   unit; minimum fidelity; and maximum staleness. Use `UNKNOWN`, `STALE`,
   `CONFLICTING`, or `NOT_APPLICABLE`; never turn absence into zero or healthy.
3. **Minimize acquisition.** Treat candidate batched reads as weighted set cover.
   Select the least-cost calls satisfying plane, fidelity, freshness, and
   consistency. Report uncovered fields; a partial cover is not complete or
   globally optimal.
4. **Freeze a bitemporal snapshot.** Record valid time (when reality held) and
   transaction time (when the project learned it), source/revision, request and
   response digests, collector identity, and uncertainty. Content-address the
   canonical snapshot. Never solve against opportunistically changing live data.
5. **Compile the decision model.** Separate hard constraints from objectives.
   Preserve opaque IDs. Choose the smallest honest method: convex/linear for
   continuous structure, graph or flow for topology, mixed-integer/discrete for
   indivisible choices, stochastic/robust for uncertainty, and control methods
   for evolving state. Name relaxations, rounding, bounds, and optimality gaps.
6. **Engineer value.** Bind each criterion to a stakeholder, required function,
   metric, unit, baseline, horizon, and provenance. Include lifecycle development,
   infrastructure, operation, support, migration, risk, and retirement cost.
   Preserve the Pareto frontier; do not invent weights or dollars for qualitative
   outcomes. Hard safety, governance, accessibility, and reliability constraints
   cannot be traded for utility.
   For metered services, keep gross usage and gross cost separate from credits,
   discounts, refunds, and net invoice amount. Measure burn rate and projected
   exhaustion; a credit can hide waste but cannot make it valuable.
7. **Use independent evaluators as sensors.** Give heterogeneous evaluators the
   same read-only snapshot and prompt hash. Record provider/model/version,
   latency, cost, findings, confidence, disagreements, and executable disposition.
   Findings are hypotheses. Track evaluator precision and unique verified defects
   before increasing its influence.
8. **Run controlled experiments when causality matters.** Preregister hypothesis,
   control/treatment, randomization and assignment unit, exposure, one primary
   metric, guardrails, minimum sample, analysis/stopping rule, horizon, and cost.
   Preserve interference, novelty, peeking, selection, and multiple-testing risks
   as explicit threats. A/B results update value evidence; they do not authorize
   rollout, and observational evidence must not be mislabeled causal.
9. **Recommend, then authorize separately.** Commit the snapshot, constraints,
   objective priority, solver/version, alternatives, bounds, Pareto set, and
   predicted outcomes to one immutable recommendation ID. Existing approval and
   execution controls remain authoritative.
10. **Measure realized outcomes.** Match exact recommendation and assignment IDs.
   Record actual time, cost, reliability, user outcome, harm, and constraint
   violations from authoritative sources. Calculate error and drift; do not
   rewrite the prediction.
11. **Improve through a trial.** Persist accepted contracts and gates in the
    project; open Git issues for unresolved hypotheses. Record append-only metrics.
    Promote a method only after its stated exit criterion; remove it when total
    cost exceeds demonstrated value.

## Cost-containment control

For any resource that can amplify work (queues, retries, schedules, fan-out,
autoscaling, telemetry, storage writes), record its owner, required function,
gross unit cost, value metric, envelope, and rollback facts. Distinguish three
levels: a dashboard observes, an alert asks a human to act, and an enforcement
control bounds or stops consumption. Never report an alert as a failsafe.

Prefer a layered dev control: fail-closed feature switch, bounded concurrency,
bounded retries, dead-lettering, daily/monthly gross-cost gate, and a reversible
environment-scoped kill switch. After applying it, re-read provider state and
measure the subsequent usage slope; configuration or API success alone is not
proof that spend stopped. Production authority remains separate.

Read [references/evidence-contract.md](references/evidence-contract.md) when
defining schemas, persistence, completeness, or evaluator records.
Read [references/theory-to-practice.md](references/theory-to-practice.md) when
framing a whole-system model, selecting mathematical methods, explaining semantic
versus opaque data, or designing the reusable development-to-production loop.

## Required handoff

Report:

- decision and authority boundary;
- snapshot ID, valid/transaction times, and coverage gaps;
- hard constraints and objective priority;
- method, lower bound, recommendation, and optimality claim;
- Pareto-efficient and dominated alternatives;
- predicted outcomes and lifecycle cost;
- realized outcomes/error when available;
- evaluator disagreements and verified dispositions;
- trial status, metric links, and open Git issues.
