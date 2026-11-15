# Release convergence view

The Releases area's Convergence tab is the existing `rm05-release-rollout`
view. It reads the visible `/v1/fleet/installations` projection, including each
installation's environment, desired and observed version, release digest,
configuration digest, health, rollout status, lifecycle and observation freshness.
It performs no write and grants no approval.

Matching versions alone are insufficient. A converged row requires a report,
`stale: false`, desired coordinates and digests, exact observed matches,
`HEALTHY`, `UP_TO_DATE`, `APPLIED` and `INSTALLED`. Missing reports, stale
observations, missing desired evidence, differing coordinates and pending
health/configuration are displayed separately. Freshness is the Hub projection's
classification, not a second browser clock threshold. An empty or unavailable
installation index cannot establish convergence.

This is a Conduit extension using native Hub fields. It replaces the old
example environment rows and connects both the standalone and area renderer.
It does not establish rollout completion, publication authority or private
protocol compatibility. The remaining scheduling, dependency and timeline
panels in issue #553 are separate work; this view does not synthesize their data.
