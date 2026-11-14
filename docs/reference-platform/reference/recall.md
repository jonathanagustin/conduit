# Recall and roll-off contracts

Sources are indexed in `review-ledger.tsv`. These notes reconstruct public
selection semantics without claiming private recall protocol compatibility.

## Ranges and identity

- Open recalls cover the starting Release and all later Releases, including
  future publication. Closed ranges exclude their ending version. Explicit
  exclusions remain eligible. A single-Release recall is represented directly
  rather than approximated with a synthetic version bound.
- Reverted recalls no longer constrain selection. Every recall has a stable ID,
  reason, strategy, and optional source evidence. Security-derived recalls bind
  to the exact normalized assessment run.

## Observed-state roll-off

- Recall behavior is determined from the version an installation actually
  reports, not only the desired catalog. Plan creation reads the latest
  Environment report before selection and records the causal recall IDs.
- Roll forward prefers the newest non-recalled Release on the tracked channel
  that meets the floor and all other constraints. If none exists, the Entity
  remains on its recalled Release rather than receiving an unsafe or impossible
  Plan.
- Allow-downgrade still prefers a newer fix, then chooses the newest safe
  Release at or above the configured minimum.
- Freeze keeps affected installed Entities on their current version until its
  exact expiry. Selected-Environment testing rolls off only named
  Environments; all others remain frozen.
- Recall roll-offs are explicit Plan metadata and bypass ramp pacing. They still
  obey approvals, maintenance, artifact policies, and suppression.

## Automatic security recall

When enabled for an Environment, a complete normalized vulnerability
assessment with FAIL and active findings derives a stable single-Release
security recall. The Hub does not invent scanner findings or scores; scanner
execution and vendor normalization remain external. A later non-recalled
Release can roll the observed installation forward. The separately reviewed
CVE-to-CVE risk-score workflow remains a narrower upstream option and is not
claimed because this platform does not reproduce Palantir's private scoring
formula.

## Local evidence

- `internal/controlplane/catalog.go` derives security recalls and performs
  observed-state selection; `lifecycle.go` supplies the persisted report.
- `catalog_test.go` covers wait-for-fix, roll-forward, bounded downgrade, timed
  freeze expiry, selected Environments, single-version security recall, causal
  Plan metadata, and lifecycle integration.
