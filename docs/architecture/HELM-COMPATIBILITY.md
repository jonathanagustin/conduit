# Helm compatibility contract

Platform's Spoke executor supports Helm 3 and Helm 4 without treating their
command-line behavior as interchangeable.

## Supported lanes

| Lane | Pinned version | Purpose |
| --- | --- | --- |
| Default runtime | Helm 4.2.3 with `oras-go` 2.6.2 | Source-built by `Containerfile.agent` from the hash-pinned Helm release with the upstream ORAS security fix, then shipped in the exact Spoke OCI digest |
| Compatibility | Helm 3.21.1 | Exercised in the Spoke E2E matrix while Helm 3 remains upstream-supported |

Both lanes pull an exact OCI chart, verify the registry-reported digest,
render and validate exact workload-image coordinates, reconcile the release,
observe attributable release state, and exercise rollback. The validating render
passes `--is-upgrade` whenever a revision already exists, so it is the form the
mutation will produce: `helm template` otherwise sets `.Release.IsInstall`, and a
chart keyed off `.Release.IsUpgrade` would be validated in one form and deployed
in another. The matrix proves
fresh Platform acceptance independently with each major. It does not yet claim
a tested in-place Helm 3-to-4 migration of an existing release.

Helm 4 removed Helm 3's `--atomic` upgrade flag in favor of
`--rollback-on-failure`, and made the apply method selectable. The executor
detects the CLI major and uses:

- Helm 3: `--atomic --wait` for managed rollout.
- Helm 4: `--rollback-on-failure --wait=watcher` for managed rollout.
- Helm 4 in either rollout mode: `--server-side=false`, preserving Platform's
  established client-side update behavior until a separately tested migration
  contract permits server-side apply.

Unsupported majors fail closed before chart execution. Helm plugins and
post-renderers are outside this contract.

## Evidence and maintenance

The unit gate is:

```sh
go test ./internal/agent -run 'TestHelmExecutor'
```

The integration gate is the two-version matrix in
`.github/workflows/e2e.yml`. Version and image updates must keep archive
checksums, source hashes, module overrides, and OCI manifest digests exact and
must rerun both lanes. Helm 3 is copied from its pinned compatibility image;
the Helm 4 lane exercises the hardened source-built binary used by releases.

Upstream references, reviewed 2026-08-02:

- [Helm 4 overview and CLI changes](https://helm.sh/docs/overview/)
- [Helm 3 end-of-life schedule](https://helm.sh/blog/helm-v3-end-of-life/)
- [Helm version-skew policy](https://helm.sh/docs/topics/version_skew/)

Helm 3 receives bug fixes through 2026-09-09 and security fixes through
2027-02-10 according to the reviewed upstream schedule. Support here remains
bounded by that upstream window and this repository's pinned compatibility
gate.
