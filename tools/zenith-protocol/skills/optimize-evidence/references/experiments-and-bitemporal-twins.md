# Experiments and bitemporal twins in practice

## Why both are needed

A twin answers “what did the system state appear to be, based on evidence known
then?” An experiment answers “did a controlled change cause an outcome?” Neither
substitutes for the other. A high-fidelity observational model can still confound
cause and correlation; an experiment without a replayable state snapshot cannot
explain where or why its result applies.

## Bitemporal fact and snapshot behavior

For every fact retain two independent clocks:

- **valid time**: the interval in which the value held in the modeled world;
- **transaction time**: when the evidence entered the project’s knowledge.

An as-of query takes both `validAt` and `knownAt`. Admit a fact only when its valid
interval contains `validAt` and its recorded time is no later than `knownAt`.
The equality boundary is load-bearing: evidence recorded exactly at `knownAt` is
known. Apply the declared freshness limit after valid-time matching.

Match entity, attribute, unit, state plane, and minimum fidelity before comparing
values. Return `UNKNOWN` when no admitted fact exists, `STALE` when matching facts
exist but exceed freshness, and `CONFLICTING` when admitted authoritative facts
disagree. Do not select the newest or most convenient value unless an explicit
source-precedence or correction contract makes that choice semantic.

Content-address each fact over its value, valid/transaction times, source and
revision, request/response digests, collector identity, fidelity, and uncertainty.
Content-address the snapshot over query times and canonical field results. Store
records immutably: write a mode-restricted temporary file, flush it, atomically
link/rename without overwrite, then flush the parent directory. A mutable current
pointer may reference a snapshot ID but cannot replace history.

## Controlled experiment behavior

Preregister before exposure:

```text
hypothesis, control/treatment, assignment unit, randomization unit,
exact allocation, one primary metric, guardrails and harm limits,
minimum sample, maximum duration, analysis revision, stopping rule
```

The treatment effect is `treatment - control`. A maximize primary passes only
when its lower uncertainty bound is strictly above zero; a minimize primary passes
only when its upper bound is strictly below zero. “No detected harm” is not the
same as “proved improvement.”

Guardrails are normally non-inferiority tests. Give each a non-negative exact harm
limit. For a maximize guardrail, the lower effect bound must be at least the
negative limit. For a minimize guardrail, the upper effect bound must be at most
the positive limit. Equality passes. This distinction avoids requiring cost,
latency, reliability, and safety guardrails all to improve significantly before a
useful treatment can be reviewed.

Reject unregistered metrics, missing effects, insufficient samples, invalid exact
bounds, changed analysis revisions, duration overruns, and unrecorded exposure
cost. Record inconclusive as inconclusive. The analysis implementation—not the
evidence envelope—owns fixed-horizon/sequential validity, multiple comparisons,
variance, clustering, interference, novelty, and sample-ratio checks; pin and test
that implementation explicitly.

## Joining the loop

The experiment plan references the pre-exposure twin snapshot. Its outcome creates
new experienced, billed, and observed facts, plus bounded value estimates. Rebuild
the Pareto frontier without rewriting the prior frontier. A treatment becomes
eligible for governance review only when the primary, every guardrail, sample and
stopping contract pass. Existing approval, staged exposure, monitoring, rollback,
and exact reported state still control execution.

Mutation-check equality and fail-closed boundaries. Temporarily change `<=` to `<`
at a non-inferiority limit and change “recorded no later than” to “recorded before”;
the focused tests must fail. Restore production source only, leaving the new tests
in place, then rerun the complete gate.
