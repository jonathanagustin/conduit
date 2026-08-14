# DORA metrics inside whole-system optimization

DORA’s current software-delivery performance model has five measures for one
application or service: change lead time, deployment frequency, failed-deployment
recovery time, change fail rate, and deployment rework rate. They describe two
factors—throughput and instability—and are useful longitudinal outcome evidence.
They are not a complete model of product or organizational value.

Authoritative definitions and cautions:

- <https://dora.dev/guides/dora-metrics/>
- <https://dora.dev/insights/dora-metrics-history/>

## Where they fit

Use DORA measures to evaluate changes to the development-to-production delivery
system: test selection, review flow, release automation, approval latency, rollout
strategy, recovery design, and AI-assisted development. Baseline one service,
change one capability or run a controlled trial, then compare later windows.

Model the five measures as an outcome vector rather than a scalar objective:

```text
minimize change-lead-time distribution
maximize deployment frequency, subject to meaningful batch semantics
minimize failed-deployment recovery-time distribution
minimize change fail rate
minimize deployment rework rate
```

Do not optimize these without product outcomes, reliability/SLOs, security,
accessibility, lifecycle cost, user harm/value, and team well-being. Faster delivery
of low-value or harmful changes is not success. A high deployment count created by
splitting no-op changes is not throughput. A recovery clock that ignores unresolved
failures is not stability.

## Evidence contract

For each deployment retain:

```text
serviceId, deploymentId, source/revision,
commitAt, deployedAt,
failed, recoveredAt,
unplannedRemediation/rework,
validAt, recordedAt
```

Define production and immediate intervention for that service before measurement.
Use half-open windows and stable deployment identity. Preserve each lead and recovery
duration; a median or percentile is a named projection, not the raw evidence. Keep
rates as exact numerator/denominator pairs and frequency as deployments plus window
duration. A failed deployment without recovery evidence makes recovery measurement
incomplete; it must not disappear from a mean.

## Important boundaries

- Measure one application/service in its own context; do not rank unlike services
  or teams.
- Do not substitute generic incident MTTR for failed-deployment recovery time. The
  latter is specifically tied to production impairment caused by a deployment.
- Deployment rework is unplanned deployment work caused by a production incident;
  keep its classification and provenance reviewable.
- DORA measures are lagging delivery outcomes and possible leading indicators for
  broader performance. Correlation does not prove that a process intervention
  caused a change.
- Use experiments or an explicit causal design where attribution matters. Pair the
  DORA primary/secondary outcomes with preregistered guardrails.
- Avoid organization-wide quotas and blended scores. Goodhart pressure turns useful
  diagnostics into gaming targets.

## Optimization loop

1. Freeze a pre-change twin snapshot and a service-scoped DORA baseline window.
2. Name the delivery constraint being changed and expected effect on all five
   measures, user value, cost, reliability, and well-being.
3. Apply approval and rollout controls independently of the recommendation.
4. Collect provenance-complete deployment events into later bitemporal snapshots.
5. Calculate exact measures and gaps; never impute unresolved recovery.
6. Compare prediction with outcome and update the Pareto frontier.
7. Retain the method only if its realized whole-system value earns its lifecycle
   cost—not merely because one DORA metric improved.
