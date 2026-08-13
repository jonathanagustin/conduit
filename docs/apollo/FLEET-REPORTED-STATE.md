# Fleet and reported-state contract

The Spoke reports each installation as independent state dimensions rather than
one overloaded status:

- immutable observed Product Release version and digest;
- observed configuration digest and config status;
- health;
- rollout status;
- installation lifecycle;
- observation timestamp.

A successful Plan requires exact Product, version, Release digest, namespace,
and configuration identity plus `HEALTHY`, `UP_TO_DATE`, `APPLIED`, and
`INSTALLED`. Health does not imply rollout eligibility: a later desired Release
can make a healthy installation `OUT_OF_DATE`, and recall can be represented
without rewriting its health.

`GET /v1/fleet/installations` projects the latest desired Plan and latest
reported Environment state into one deterministic installation list. It
includes desired-but-unobserved installations as unknown/stale, detects Release
and config drift, reports maximum result age, and summarizes each status axis.
Exact filters are available for `environment`, `product`, `health`,
`rolloutStatus`, `configStatus`, and `lifecycle`; `maxAgeSeconds` marks old
observations stale.

This is a query projection over canonical Plan/report records, not a second
desired-state store. Flux reports Kubernetes source/application reconciliation,
while the Platform Spoke reports Product installation identity and outcome.

Annotated HTTPS health checks, quorum aggregation, the full health taxonomy,
bounded evidence, observed-state recall derivation, and individually governed
bulk actions feed this projection. Owner/team display enrichment remains a UI
projection concern; authorization continues to use canonical resource grants.
