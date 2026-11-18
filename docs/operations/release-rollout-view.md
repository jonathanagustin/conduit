# Release rollout read contract

`GET /v1/products/{product}/releases/{version}/rollout` is a Conduit-native,
read-only aggregation for #415. The response has `valueTier: CONDUIT_NATIVE`;
`delivery.ReleaseRolloutView` records its scope in the invention register. It
does not implement or claim a private Apollo endpoint.

The caller needs an explicit authenticated operator identity and the native
Product `resource:read` grant (or native administrative authority). A missing
localhost development header is still anonymous. Environment facts require
that Environment's native read authority. The original carrier and delegated
principal are preserved; resolving a second identity partway through a read is
not permitted. Both query parameters and request bodies are rejected.

The response contains:

- `product`, `version`, `readAt` and `valueTier` identifying this read.
- `environments`: visible Environment IDs, `installations` and
  `promotionGraphs`. Installation fields preserve the existing fleet projection:
  desired and observed versions, release and configuration digests, observation
  time/age, stale flag, health, rollout, configuration and lifecycle states.
  A row is included when either its desired or observed version matches the
  requested release. An absent or zero observation time and empty observed
  coordinates remain unknown; they do not establish a successful deployment.
- `promotionGraphs`: the requested Product's declared pipeline on each visible
  Environment's latest native Plan. Each names its `planId`, `enabled` flag and
  directed `fromChannel`/`toChannel` edges. These settings are Product-scoped,
  not specific to one release version. Graph topology is not a promotion
  evaluation, a current pause/gate result, or proof of channel reachability.
- `rollouts`: matching staged rollout IDs, statuses and ordered waves, including
  their recorded membership, gate and status. Wave order is preserved; no
  dates, percentages, schedules or dependency edges are inferred.
- `rolloutPersistence: process-local`: the existing wave service is shared with
  `/v1/rollouts` in this Hub process. It is not restart-safe history or a
  multi-replica inventory. Empty results describe this store, not proof that no
  rollout has ever existed.

A rollout containing any unreadable Environment is omitted in full. Merely
redacting its membership would still expose aggregate status derived from
hidden members. No unfiltered totals, hidden member names or free-text wave
reasons/authors are returned. Installation, graph and rollout collections are
ordered deterministically, while the native wave order remains unchanged.

The adapter reuses one plans read and one reports read for the native catalog
and fleet projections. Publication metadata and wave reads are independent;
`readAt` is the observation clock, not a transactional snapshot or execution
receipt. The response is `Cache-Control: no-store`. Unknown release coordinates
return 404 after Product authorization; missing identity returns 401 and missing
Product authority returns 403. Storage failures or an authorized projection
above the 16,384-row bound return a sanitized 503, not truncated data or an
empty successful view. Rows, graph edges, waves, wave members and output
Environment records count toward that bound.

This route neither creates a Plan/Change Request nor advances a rollout or
promotion. Native authorization audit records remain permitted. Existing write
routes and their explicit approval rules remain the only mutation paths.

Tests exercise native desired/reported joins, latest Plan graph selection,
shared wave reads, unknown observations, Product denial, Environment filtering,
mixed-visibility rollout omission, anonymous localhost requests, request input
rejection, single identity resolution, source failures, bounds and detached
output slices. Complete local CI and targeted negative controls gate promotion.

## Stored blackout intervals in Windows

The existing Environment/Windows calendar requests
`GET /v1/blackouts?environment=<id>&includeIntervals=true`. The default request
still returns only the evaluated `environment`, `blocked` and `reason` fields.
The opt-in native snapshot adds the evaluation timestamp and up to 64 stored
intervals for exactly that environment, ordered by start. `truncated: true`
means more exist; the blocked decision still evaluates the complete stored set.
The route retains its authenticated operator-any read contract. A deployment
without the snapshot adapter returns 501 rather than an empty list.

The calendar uses exact stored start/end coordinates, reason and declaring
operator, with hatching for blackout intervals. It labels `CONDUIT_NATIVE`
process-local custody and truncation: the service's declarations disappear on
Hub restart, and this display does not establish durable scheduling. A legacy
decision-only response, wrong-environment interval, malformed coordinates or
failed read produces an unavailable-evidence label and no blackout span. An
explicit empty native snapshot is distinguished from unavailable evidence.

This adds no declaration, clearing, approval or execution control. Blackout
spans remain distinct from recorded maintenance overrides, weekly rules,
rollout appointments and resolved dependency timing. The latter two are not
inferred from wave order or the current blocked reason.
