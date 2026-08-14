# Portable optimization evidence contract

Use project-native types, but preserve these semantic records.

## State requirement

```text
id, entity/attribute, statePlane, unit, authoritativeSource,
minimumFidelity, maximumStaleness, consistencyRequirement
```

State planes are `DESIRED`, `ADMITTED`, `EXECUTING`, `OBSERVED`, `BILLED`, and
`EXPERIENCED`. Knowledge status is `KNOWN`, `UNKNOWN`, `STALE`, `CONFLICTING`,
or `NOT_APPLICABLE`. State plane and knowledge status are independent.

## Observation and bitemporal fact

```text
factId, entityId, attribute, value, unit, statePlane, knowledgeStatus,
validFrom, validUntil, recordedAt, supersededAt,
source, sourceRevision, requestDigest, responseDigest, collectorIdentity,
fidelity, uncertainty
```

Keep raw signed or content-addressed evidence outside the fact row. The row
indexes it; it does not replace it. Corrections append a later transaction-time
version instead of rewriting history.

## Recommendation

```text
recommendationId, snapshotId, createdAt, decisionVariables,
hardConstraints, objectivePriority, solverIdentity, relaxation,
lowerBound, assignments, alternatives, paretoFrontier,
predictedOutcomes, predictedLifecycleCost, assumptions, unknowns
```

Canonicalize unordered sets before hashing. Include units. `optimal` requires a
complete feasible model and a proved exact optimum; otherwise report the bound,
gap, heuristic, or partial scope explicitly.

## Outcome

```text
recommendationId, observationSource, observationRevision, observedAt,
exactAssignmentIds, realizedOutcomes, realizedLifecycleCost,
constraintViolations, signedError, absoluteError
```

Reject partial or mismatched outcome evidence unless the record explicitly says
it is a partial observation and excludes aggregate accuracy claims.

## Metered-resource value control

```text
controlId, environment, resourceId, owner, requiredFunction, valueMetric,
grossUsage, grossUnitCost, creditsDiscountsRefunds, netCost, burnRate,
forecastHorizon, softAlertThreshold, hardEnvelope, enforcementAction,
rollbackFacts, authoritativeUsageSource, authoritativeControlSource,
observedBefore, observedAfter, realizedUsageSlope
```

Credits and net invoices never replace gross usage/cost. An alert is not an
enforcement action. Preserve the exact pre-change state and independently re-read
the provider state after mutation; later usage slope is the outcome evidence.

## Controlled experiment

```text
experimentId, hypothesis, control, treatment, assignmentUnit,
randomizationUnit, allocation, exposureStart/End, primaryMetric, guardrails,
minimumSample, analysisId, stoppingRule, effectEstimate, uncertaintyBounds,
sampleCounts, exposureCost, threats, outcomeRevision
```

Preregister the plan before exposure. Keep assignment and analysis revisions,
including exclusions, durable. Require the primary result and all guardrails;
inconclusive is not failure or success. Record spillover/interference and never
promote an observational comparison to causal evidence.

## Evaluator observation

```text
evaluationId, snapshotId, promptDigest, provider, model, version,
startedAt, finishedAt, cost, findings, confidence, disagreements,
disposition, executableEvidence, laterCorrectness
```

Do not include credentials or raw secret-bearing context. Evaluator votes are
uncalibrated observations until executable evidence establishes their accuracy.

## Durable storage

Use immutable content-addressed records plus append-only indexes. A mutable
"current" pointer may reference the latest record but must not replace history.
Git is appropriate for compact contracts, decisions, fixtures, and metrics;
large/raw telemetry belongs in its authoritative store with digests committed
to the project. Git issues hold unresolved work, not ground-truth state.
