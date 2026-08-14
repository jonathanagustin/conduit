# From whole-system theory to measured decisions

## What we are trying to do

“Model the whole system” does not mean build a perfect copy of reality. No finite
model is complete, and collecting everything can cost more than the decision is
worth. The practical goal is a **decision-focused digital twin**: for a named
decision and horizon, retain every state, constraint, uncertainty, cost, and
stakeholder outcome that could change the choice. Make omitted or stale knowledge
visible, then improve fidelity where its expected decision value exceeds its
acquisition and maintenance cost.

The system includes development and production together. Code, tests, people,
queues, compute, storage, networks, suppliers, money, user experience, risk,
governance, migration, support, and retirement are coupled flows through space
and time. Optimizing a local metric can displace cost or harm elsewhere, so the
unit of success is realized stakeholder value subject to hard boundaries—not
build speed, utilization, revenue, or evaluator agreement in isolation.

## Mathematical shape

Represent the system at a decision time as:

```text
state x(t), controls u(t), disturbances w(t), observations y(t)
transition x(t+1) = f(x(t), u(t), w(t))
hard feasible set (x,u) ∈ C
value vector V = [user outcome, reliability, latency, money, labor, risk, ...]
```

This is generally not one convex optimization problem. Use compositional models:

- graphs and flows for dependencies, networking, provenance, and movement;
- convex/linear programs for divisible allocation, rates, and capacity;
- mixed-integer or constraint programs for discrete placement, ordering, and
  yes/no choices;
- queueing and simulation for contention, tails, and emergent latency;
- stochastic or robust optimization for uncertainty;
- control and online optimization for state that evolves after a decision;
- causal experiments for effects the observational twin cannot identify.

Convex relaxations remain valuable around discrete problems: they provide lower
bounds, infeasibility certificates, sensitivity, and a measurable rounding gap.
Call a result optimal only inside its stated model and completeness boundary.

## Semantic state versus opaque coordinates

Use **semantic fields** where meaning is part of the contract: desired versus
observed state, dollars versus seconds, valid time versus recorded time, approved
versus executing. Their meaning lets constraints compare them safely.

Keep **opaque coordinates** opaque when only identity is known: release IDs,
mutant IDs, tenant IDs, digests, or upstream versions. Equality and exact lookup
are valid; ordering, proximity, compatibility, and business meaning are not
unless a separate proven contract supplies them. This prevents an optimizer from
turning a convenient string or integer representation into invented semantics.

## The closed evidence loop

1. Frame one decision, its owner, horizon, authority, rollback, and value criteria.
2. Declare required semantic state and preserve unknowns.
3. Optimize observation calls as a costed coverage problem; acquire only sufficient
   authoritative state and record gaps.
4. Freeze a bitemporal, content-addressed snapshot so every answer is replayable.
5. Compile hard constraints separately from objective priorities and alternatives.
6. Solve with the smallest honest mathematical method; retain bounds and Pareto
   alternatives instead of hiding trade-offs in an arbitrary scalar.
7. Use heterogeneous evaluators as fallible sensors. Verify findings with executable
   evidence and calibrate each evaluator's precision, unique yield, latency, and cost.
8. Where causality matters, preregister an A/B or other controlled experiment with
   assignment, exposure, primary outcome, guardrails, stopping rule, and analysis.
9. Authorize through the system's existing governance; a recommendation is evidence,
   never permission.
10. Match realized outcomes and lifecycle cost back to the exact recommendation.
    Record error, drift, violations, and user effects without rewriting predictions.
11. Update the durable Pareto frontier, model calibration, assumptions, and method
    trial. Remove measurements or evaluators that no longer improve decisions.

## Durable implementation pattern

Store compact contracts, MADRs, fixtures, PDRs, and append-only metrics in Git.
Use Git issues for unresolved work. Store high-volume telemetry in its authoritative
database/object store and commit immutable digests and query/collector revisions.
Every fact needs valid time and transaction time; every prediction needs an outcome
join key; every cost needs currency, unit, source, and horizon; every mutable
“current” view must be derivable from immutable history.

Human-readable projections such as a GitHub Wiki may improve discovery, but they
must be reproducibly published from versioned canonical sources and link the exact
source revision. Treat the projection as a read model, never a second writable
authority for contracts or evidence.

A portable platform can expose adapters for observations, solvers, experiments,
evaluators, and outcome sources while leaving each product's schemas, credentials,
catalogs, environments, approval, and execution in that product's repository. The
Zenith protocol supplies the loop and evidence semantics; project-native code and
tests supply authority and ground truth.

## How to know the process itself is improving

Measure decision quality, not documentation volume. Useful longitudinal measures
include observation coverage and cost, prediction error, constraint violations,
Pareto-frontier change, experiment exposure cost, time-to-decision, rollback/harm,
evaluator verified precision and unique defect yield, and total engineering cost.
Treat the method as another controlled system: trial a change, compare realized
value with its burden, retain it only while it earns its lifecycle cost.
