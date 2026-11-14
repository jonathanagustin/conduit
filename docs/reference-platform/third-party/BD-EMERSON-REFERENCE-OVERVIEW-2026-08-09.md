# BD Emerson Apollo overview evidence

Source: [Palantir Apollo, Explained](https://www.bdemerson.com/article/palantir-apollo),
Leslie Sakal, Managing Director, BD Emerson. Captured 2026-08-09 UTC.

This is a third-party explanatory source, not Palantir documentation, a public
API specification, a conformance suite, or direct tenant observation. The page
renders a 2026-08-04 date while its JSON-LD declares both publication and
modification on 2026-07-29. The captured HTML was 286,135 bytes with SHA-256
`ba29d052deae2fbc8ec753a93ffee623c705baa572cee6f9c4c086bb27c0fe34`.

## Bounded observations

The article describes heterogeneous cloud, on-premise, disconnected, maritime,
and edge delivery; Environment-local pull execution; versioned declarative
state; constraints over dependencies, maintenance windows, approvals, and
ordering; progressive promotion with health evaluation; rollback expressed as
control-plane state; audit history; fleet management; and artifact provenance.
These are observations of the article's claims, not independently verified
Apollo contracts.

Its singular “intended state” language is an explanatory simplification.
Official evidence and Platform's executable model retain Release Channel
selection, constraint evaluation, issued Plans, exact reported state, and
rollback rather than one unconditional global desired version. Claims about
signing, accreditation, commercial packaging, or private control-plane behavior
require authoritative evidence before adoption.

## Media and reuse boundary

The only article-specific media is an editorial architecture SVG, not product
UI. It depicts a control plane, versioned releases, connected target classes,
and a controlled-transfer path to a disconnected target. Its arrows are
illustrative and cannot override the prose or official outbound-only Spoke
boundary.

Two CDN URLs returned byte-identical 3,605-byte SVGs with SHA-256
`c9bac7c75c214a1d4a3f86e2a31cafe0670fed472c6113bc0bf3e832c123473b`;
the canonical inventory is in `evidence-manifest.json`. The site's
[terms](https://www.bdemerson.com/terms-and-conditions), captured with SHA-256
`a8a78d2d46577a924ecc7288c033d505b71bc4aa97990d2f57b0f437849d4172`,
reserve text and graphics and prohibit republication without permission.
Neither the article body nor SVG is stored in this repository.

## Disposition

- Architecture corroboration: useful but secondary.
- H60 UI fidelity: unchanged; no product UI was observed.
- H61 CLI fidelity: unchanged; no CLI behavior or format was observed.
- Public/private compatibility: unchanged; no API, wire, profile, or archive
  evidence was observed.
- Optimization intake: version divergence, unreachable-Environment count,
  support/backport cost, topology complexity, promotion-health quality, and
  change-control evidence completeness are candidate measures, not facts
  supplied by this source.
