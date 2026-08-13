# Historical Apollo evidence, 2020–2022

Status: public, historical, bounded visual and semantic evidence.

The 2020 article's two exact screenshots are retained under
`evidence/legacy-apollo-2020/`. Its editor note says the product suite has since
changed, so newer documentation remains authoritative for current composition.

## Durable visual conclusions

The Product view combines a fixed resource summary and release/change feed with
a dense installation table. Tabs separate Rollout, Availability, Upgrades,
Releases, Monitors, Installs, and Remote Installs. A selected installation owns
a lower detail region with Info, Upgrade, Config, and Logs. The Environment
view similarly combines a fixed summary/filter pane with Events, Suppressions,
Invalid installs, Monitors, and Plans. Events have a time overlay, facet counts,
category/severity/installation grouping, and a dense attributable event table.

These screens corroborate split list/detail composition, compact status tags,
resource-scoped tabs, filter facets with counts, timeline plus table dual views,
and keeping selected-record evidence in context. They also show why every large
index and log/event source needs server bounds. They do **not** justify copying
the obsolete navigation, labels, private routes, dark-only palette, or visual
table semantics.

## Orchestration semantics

Palantir's 2022 article, [“Palantir Apollo Orchestration: Constraint-Based
Continuous Deployment For Modern Architectures”](https://blog.palantir.com/palantir-apollo-orchestration-constraint-based-continuous-deployment-for-modern-architectures-cdf42da19ba4),
describes a control loop with three distinct inputs: catalog releases, declared
Environment state, and Agent-reported current state. Software and Environment
constraints limit recommended transitions toward target state. Its examples
apply service-dependency constraints in both upgrade directions and validate
schema-read compatibility before rollback.

That model supports Platform's existing desired/reported split, dependency
ordering, constraint evidence, and rollback admission. It does not establish a
private solver, wire contract, or permission model. UI consequences are bounded:
show all three input provenances, the constraint that blocks or permits a step,
and why a rollback candidate is safe; never reduce an orchestration decision to
an unexplained status color.
