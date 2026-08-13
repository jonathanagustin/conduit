# Health and status contracts

Sources are indexed in `review-ledger.tsv`; the pinned public OpenAPI is under
`../snapshots/`.

- Public status separates Entity topology (`entities/info`) from periodic node
  and container health (`entities/health`).
- A health check preserves ID, message, typed parameter object, and one of
  `HEALTHY`, `REPAIRING`, `WARNING`, `ERROR`, or `TERMINAL`.
- Containers independently report liveness and readiness. Entity aggregation
  uses `UNKNOWN`, `ANY`, `ALL`, `ALL_BUT_ONE`, `SIMPLE_MAJORITY`, or
  `NO_GUARANTEE` quorum semantics.
- Health payloads are bounded to 10,000 bytes per Entity. One `entities/health`
  element exists for every (Product, node), so the bound governs a single
  Entity's evidence and never how many nodes an Environment may report; the
  request itself is bounded like `entities/info`. Health remains independent of
  rollout, configuration, lifecycle, and recall state.

## Local evidence

- The public adapter strictly decodes and retains topology and full check
  details, rejects invalid/duplicate records, enforces the payload bound, and
  projects aggregate health into the canonical Environment report.
- Existing Agent-owned rollout and configuration observations are preserved
  when a health-only report arrives.
- `apollo_api_test.go` covers detailed WARNING evidence, `ALL` aggregation,
  invalid result rejection, per-Entity oversized payload rejection, and a
  fifty-node fleet whose request exceeds one Entity's bound.

## Active Product-side probing

- The Spoke discovers managed Pods by installation label and reads the public
  `apollo.palantir.com/health.probe` annotation. Its strict `containerProbes`
  mapping names the container, context path, and port.
- Probe URLs are always HTTPS and resolve to
  `<context-path>/status/health`. Paths, ports, container references, Pod IPs,
  and annotation size are validated before network access. A private CA may be
  mounted; certificate verification is never disabled.
- The Agent queries at a five-second cadence, preserves valid check metadata,
  redacts transport/HTTP failures into synthetic ERROR checks, and replaces an
  oversized response with `HEALTH_CHECK_MAX_LEN` WARNING evidence.
- An Environment-authenticated Agent fetches targets only from the latest
  succeeded Plan and submits health-only observations. The Hub recomputes node
  and quorum health, bounds each Entity detail payload to 10,000 bytes, and
  preserves rollout, configuration, and lifecycle axes.

`internal/agent/health_test.go` covers annotation discovery, canonical HTTPS
construction, strict path and response handling, payload bounds, error
redaction, and the five-second floor. `internal/controlplane/health_test.go`
covers target authority, quorum recomputation, stale-axis preservation, and
fail-closed admission.
