# Rubix Kubernetes substrate

Ground truth recovered from Palantir's compute substrate library
`com.palantir.rubixlib`, fetched from the tenant's artifacts API on 2026-08-13.
Nothing here is authored; it is Palantir's own deployment-topology model carried
as schema rather than re-inferred.

## What is here

- `crds/` — the eight CustomResourceDefinitions carried in
  `com.palantir.rubixlib:rubix-lib-core:2.512.0`. They declare what an instance
  group, node group, pod group and scheduling demand carry, and how ingress,
  egress and cross-namespace discovery are expressed:

  | Kind | Group | Version | Short |
  | --- | --- | --- | --- |
  | InstanceGroup | compute.palantir.com | v1alpha1 | ig |
  | NodeGroup | compute.palantir.com | v1alpha1 | ng |
  | Demand | scaler.palantir.com | v1alpha2 | dem |
  | PodGroup | scheduling.x-k8s.io | v1alpha1 | pg, pgs |
  | AllowlistedConnectionPolicy | policy.rubix.palantir.com | v1alpha1 | acp |
  | AllowlistedEgressPolicy | policy.rubix.palantir.com | v1alpha1 | aep |
  | CrossNamespaceDiscoveryTarget | deploy.palantir.com | v1alpha1 | cndt |
  | DomainIngress | ingress.deployability.palantir.com | v1alpha1 | di |

- `crds-manifest.json` — the provenance ledger. For each CRD it records the
  sha256 of the file, its group, kind, plural, short names, scope, served
  versions, and the top-level spec fields the recovery enumerated. This is the
  contract `scripts/verify-rubix-crds.rb` enforces.

- `http-contract.json` — the fourteen Conjure endpoints (across the egress and
  ingress allowlist services, metadata retrieval, and the user-credential
  manager provisioning/validation services) recovered from `network-api` and
  `user-credential-manager`. This is the request surface that drives the CRDs.
  It is captured here as ground truth; it is not yet modeled in
  `internal/computecontrol` (hypothesis B11).

## Gate

`ruby scripts/verify-rubix-crds.rb` cross-checks every CRD against the manifest
and fails on any drift away from the recovered schema. It runs in
`scripts/verify-local.sh` and `.github/workflows/platform-ci.yml` as
**Validate Rubix substrate CRDs**. Set `KUBECTL_BIN` to also dry-run each CRD
against a reachable apiextensions server.

## Server-side validation

All eight were applied to a real k3s v1.36.3 API server on 2026-08-14:
`kubectl apply --dry-run=server` accepted every one, `apply` registered the
api-resources with the recovered groups and short names, and sample
`InstanceGroup` (`instanceTypes: ["m5.xlarge"]`) and `Demand`
(`instance-group`, `units: []`) custom resources were created with schema
enforcement confirmed — wrong field types were rejected.

## Reproducing the source

The jars are not committed (760 KB of CFR-decompilable bytecode, reproducible
from the coordinates in `crds-manifest.json`'s `provenance.coordinates`). Each
is one authenticated request:

```
GET /artifacts/api/repositories/{rid}/contents/release/maven/{path}
Authorization: Bearer $PALANTIR_TOKEN
```

No single repository serves all five coordinates — each of the tenant's
repositories is an independent cache holding a different snapshot, so a 404 in
one is not absence. Probe the exact coordinate across every repository before
concluding it is unavailable.
