# Value engineering without hiding trade-offs

## Start with functions and stakeholders

Value is not a synonym for revenue, velocity, or utilization. For each decision,
name the stakeholder and user segment, the function they need, its criticality,
and an outcome with a unit, baseline, target, source, and horizon. Keep safety,
approval, privacy, compliance, rollback, and other non-negotiable requirements as
hard constraints. An attractive score cannot compensate for violating one.

Model benefits and harms as separate criteria when they affect different people
or have different units. Preserve qualitative judgments as explicit ordinal
scales and unknowns; do not invent prices for them. Cardinal quantities use exact
units. Estimates record point values or distributions and bounds, plus the source
revision and observation time that produced them.

## Count the lifecycle, not the purchase

Cost evidence covers development, infrastructure, operations, support, risk,
migration, and retirement over a declared horizon. Money needs an exact currency
and pricing source. Labor needs a stated unit and costing contract. Record
time-to-value and the discount rate and basis instead of silently preferring near
or distant outcomes.

Predicted and realized cost are different records. Join the realized record to
the immutable prediction, calculate error, and leave the prediction unchanged.
This makes optimism bias and model drift measurable.

## Keep the Pareto frontier durable

First remove alternatives that violate hard constraints or lack required
evidence. Among the remainder, one alternative dominates another only when it is
no worse on every criterion and better on at least one. With uncertain estimates,
use a declared robust rule, such as comparing conservative bounds. Persist the
criteria, alternatives, frontier, dominated set, infeasible set, evidence gaps,
and model revision under a content-derived identity.

The frontier is a time-indexed evidence record, not a permanent answer. Recompute
it when facts, alternatives, constraints, or model revisions change. Keep the old
record so a later decision can be explained from what was known at the time.

## Test preferences and assumptions separately

If a choice among non-dominated alternatives requires stakeholder preferences,
make every weight explicit and provenance-pinned. Normalize a cardinal criterion
against its declared baseline-to-target improvement before combining unlike
units. Run several plausible policies and report whether the selected set changes.
Never apply a policy score to an infeasible alternative.

Assumption sensitivity is a different test. Define complete scenarios with pinned
sources, retain the same alternative identities, and recompute the frontier for
each. Report which frontier members change. Keeping identities fixed prevents an
inventory change from being misreported as sensitivity to an assumption. Do not
assign scenario probabilities unless evidence supports them.

## Turn forecasts into calibration

For every criterion, record the realized outcome with the same unit, stakeholder,
horizon, and alternative identity. Calculate signed and absolute prediction error
using exact arithmetic where the source is exact. Track constraint violations,
time-to-value, lifecycle cost, and harm alongside benefit. Use controlled
experiments when observational evidence cannot identify causality.

A process improvement earns adoption when these records show better realized
stakeholder value or lower decision cost without worse guardrails. Documentation
volume and frontier churn are diagnostics, not objectives.

## Minimum executable contract

An implementation should fail closed unless it can produce:

- complete criterion and alternative contracts with provenance and horizons;
- hard-constraint, insufficient-evidence, dominated, and Pareto classifications;
- exact lifecycle-cost categories and discount assumptions;
- explicit policy-weight and assumption-scenario sensitivity results;
- immutable predicted records joined to complete realized outcomes; and
- tests proving a hard-infeasible alternative cannot win and a targeted mutant
  is killed by the assertion intended to constrain it.

The optimizer recommends evidence for review. It does not approve, deploy, or
rewrite the authority boundaries of the project that adopts it.
