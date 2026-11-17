# Release and publication

Release and publication require an explicit instruction separate from approval
to implement or merge code. The repository is proprietary. Publish only to the
approved private destination and preserve third-party license notices.

Before a release:

1. identify the exact commit and semantic tag;
2. run local verification and a deliberately dispatched release gate;
3. when a production Containerfile or runtime-base pin changed, build every
   published architecture before cutting the version tag; an amd64-only local
   build does not prove the publication matrix;
4. confirm the target registry, repository visibility, and retention policy;
5. publish immutable digests with the available SBOM, provenance, signature,
   and attestation artifacts;
6. verify the published objects by digest from a clean client.

## Cutting a release: one instruction

Land the `VERSION` bump the ordinary way — pull request, green Trunk gate,
squash merge — and then give one instruction:

```bash
gh workflow run platform-release.yml \
  --repo jonathanagustin/conduit \
  --ref main \
  --field version=<X.Y.Z>
```

That dispatch is the release decision, and it is the only one. The workflow
refuses before it creates anything unless all of the following hold for the
commit `main` points at:

- the version given equals `VERSION` at that commit, spelled as three numbers
  with no leading `v` (the Flux source policy trusts only
  `refs/tags/v[0-9]+.[0-9]+.[0-9]+`, so a prerelease tag would publish artifacts
  production then refuses to verify);
- `vX.Y.Z` does not already exist — a published tag is an identity, never moved;
- the commit is contained in `main`;
- the `Trunk gate` commit status is `success`. Pull requests are squash-merged,
  so the commit on `main` is an object no gate has seen; the verdict lives on
  the pull-request head the gate authenticated, and the check follows the merge
  back to it when exactly one merged pull request names this commit as its merge
  commit.

Only then does it create `vX.Y.Z`, read the tag back to confirm it names the
verified commit, and dispatch `platform-images.yml` against the tag. It does not
wait for publication: that run is tens of minutes and a second job watching it
bills a second runner for all of them. Watch the publication run itself, and
verify the published objects by digest as above.

Publication stays manual (ADR 0001; A5). One instruction, never zero: nothing
here is triggered by a push, a schedule, or a merge.
`scripts/verify-release-workflow.rb` holds both workflows to that shape and its
self-test exercises each refusal, because a release path can only be rehearsed
against mutated inputs — cutting a real release to test it is the thing being
tested.

Dispatching `platform-images.yml` by hand is no longer a supported release step
and now fails immediately from any ref that is not a `vX.Y.Z` tag agreeing with
`VERSION`. It used to succeed there: the image push was not tag-gated while the
runtime source, charts, offline-bundle packages, CLI and GitHub release were, so a
branch dispatch published `<component>-sha-<commit>` images, skipped everything
else, and reported success for a fraction of a release.

Credentials stay in the environment or the approved secret provider. It is
acceptable to load a repository `.env` file by reference when the user has
authorized publication, but never print, copy, commit, or include its values in
logs. A successful build or upload does not authorize deployment to a tenant.

Rollback means selecting a previously verified immutable artifact through the
owning deployment repository and approval path. Do not overwrite a published
tag to simulate rollback.

Release preparation can be proposed by Release Please; see
[`release-preparation.md`](release-preparation.md). Its PR-only workflow does
not replace the manual release instruction or any publication precondition.

## Installing and verifying a published release

The next publication attaches the following descriptor set to the GitHub Release
for `v<X.Y.Z>`. The existing `v0.17.5` release predates the complete standalone
CLI assets and signed asset inventory; it cannot pass this new verifier.
Release preparation and publication remain separate manual operations.

- `platform-oci-manifest.json` — digest-pinned Hub and Spoke container image
  references (`ghcr.io/jonathanagustin/conduit@sha256:...`) and per-architecture
  SBOM/Grype SHA-256 digests;
- `platform-helm-charts.json` — digest-pinned OCI chart references
  (`ghcr.io/jonathanagustin/conduit/platform-hub:<X.Y.Z>@sha256:...` and
  `platform-spoke`), plus `platform-hub-<X.Y.Z>.tgz`, `.sha256`, and
  `.spdx.json`;
- `platform-runtime.json` — digest-pinned Flux OCI artifact reference;
- `zarf-package-platform-runtime-<arch>-<X.Y.Z>.tar.zst` and `.sha256`;
- `conduit_<X.Y.Z>_<os>_<arch>.tar.gz` (or `.zip`), `.sigstore.json`,
  `.provenance.sigstore.json`, `.slsa.json`, `.spdx.json`, and `SHA256SUMS`;
- `release-assets.sha256` and `.sigstore.json` — a complete checksum inventory
  signed by the release workflow, binding every other attachment to its identity.

### 1. Download and verify release assets from a clean client

```bash
# Use a published version that includes the signed inventory. 0.19.0 is the
# pending release proposal, not a claim that publication has already run.
VERSION=0.19.0
REPO=jonathanagustin/conduit
ISSUER=https://token.actions.githubusercontent.com
IDENTITY="https://github.com/${REPO}/.github/workflows/platform-images.yml@refs/tags/v${VERSION}"

mkdir -p "/tmp/conduit-release-${VERSION}"
gh release download "v${VERSION}" --repo "${REPO}" --dir "/tmp/conduit-release-${VERSION}"

# The repository verifier requires Cosign, verifies the signed complete asset
# inventory first, then checks manifests, chart/Zarf/CLI checksums, SPDX/Grype
# hashes, CLI signatures and SLSA provenance bundles. No verification step is
# silently skipped when a tool is missing.
RELEASE_VERSION="${VERSION}" GITHUB_REPOSITORY="${REPO}" \
  ./scripts/verify-published-release.sh "/tmp/conduit-release-${VERSION}"
```

To verify an individual CLI archive, signature, SLSA provenance bundle, and
SPDX SBOM directly without a repository checkout:

```bash
cd "/tmp/conduit-release-${VERSION}"
cosign verify-blob \
  --bundle release-assets.sha256.sigstore.json \
  --certificate-identity "${IDENTITY}" \
  --certificate-oidc-issuer "${ISSUER}" \
  release-assets.sha256
sha256sum -c release-assets.sha256

ARCHIVE="conduit_${VERSION}_linux_amd64.tar.gz"
cosign verify-blob \
  --bundle "${ARCHIVE}.sigstore.json" \
  --certificate-identity "${IDENTITY}" \
  --certificate-oidc-issuer "${ISSUER}" \
  "${ARCHIVE}"

cosign verify-blob-attestation \
  --bundle "${ARCHIVE}.provenance.sigstore.json" \
  --type slsaprovenance1 \
  --certificate-identity "${IDENTITY}" \
  --certificate-oidc-issuer "${ISSUER}" \
  "${ARCHIVE}"

jq -er '.spdxVersion, .name' "conduit_${VERSION}_linux_amd64.spdx.json"
```

### 2. Verify and install the Helm charts and container images by digest

Read the exact chart and image digests from the verified release manifests so
neither Helm nor Kubernetes resolves a mutable tag:

```bash
HUB_IMAGE="$(jq -er '.images.hub.reference' platform-oci-manifest.json)"
SPOKE_IMAGE="$(jq -er '.images["spoke-agent"].reference' platform-oci-manifest.json)"
HUB_CHART="$(jq -er '.charts[] | select(.component == "hub") | .reference' platform-helm-charts.json)"
SPOKE_CHART="$(jq -er '.charts[] | select(.component == "spoke") | .reference' platform-helm-charts.json)"

# Verify container image signatures and attached SPDX/Grype attestations:
for ref in "${HUB_IMAGE}" "${SPOKE_IMAGE}"; do
  cosign verify \
    --certificate-identity "${IDENTITY}" \
    --certificate-oidc-issuer "${ISSUER}" \
    "${ref}"
  cosign verify-attestation \
    --type spdxjson \
    --certificate-identity "${IDENTITY}" \
    --certificate-oidc-issuer "${ISSUER}" \
    "${ref}"
  cosign verify-attestation \
    --type https://platform.delivery/attestations/grype/v1 \
    --certificate-identity "${IDENTITY}" \
    --certificate-oidc-issuer "${ISSUER}" \
    "${ref}"
done

# Verify OCI Helm chart signatures by digest:
for chart_ref in "${HUB_CHART}" "${SPOKE_CHART}"; do
  cosign verify \
    --certificate-identity "${IDENTITY}" \
    --certificate-oidc-issuer "${ISSUER}" \
    "${chart_ref}"
done

# Install the local chart archives authenticated by the signed inventory.
# This avoids resolving the mutable OCI tag again after checking its digest.
helm upgrade --install platform-hub "./platform-hub-${VERSION}.tgz" \
  --namespace platform-system \
  --values hub-values.yaml \
  --set-string "image=${HUB_IMAGE}"

helm upgrade --install platform-spoke "./platform-spoke-${VERSION}.tgz" \
  --namespace environment-system \
  --values spoke-values.yaml \
  --set-string "image=${SPOKE_IMAGE}"
```

### 3. Verify and deploy the air-gap Zarf package

```bash
PACKAGE="zarf-package-platform-runtime-amd64-${VERSION}.tar.zst"
sha256sum -c "${PACKAGE}.sha256"

zarf package verify "${PACKAGE}" \
  --certificate-identity "${IDENTITY}" \
  --certificate-oidc-issuer "${ISSUER}"

zarf package deploy "${PACKAGE}" --confirm \
  --verify=always \
  --certificate-identity-regexp "^${IDENTITY}$" \
  --certificate-oidc-issuer-regexp "^${ISSUER}$"
```

## Upgrade and version-skew policy

- **Pre-1.0 semantic versioning:** While major version is `0`, minor bumps
  (`0.N.0`) may carry breaking contract or configuration changes; patch bumps
  (`0.N.P`) carry backward-compatible fixes (`release-please-config.json` sets
  `bump-minor-pre-major=true`).
- **Supported Hub/Spoke version skew (`N` and `N-1`):** The Hub and outbound
  Spoke Agent are tested across one adjacent release in both directions — a
  previous-release Spoke against a current Hub, and a current Spoke against a
  previous-release Hub — via [`scripts/test-version-skew.sh`](../../scripts/test-version-skew.sh)
  ([`version-skew-rehearsal.md`](version-skew-rehearsal.md)). Do not run a Spoke
  more than one release behind or ahead of its Hub.
- **Upgrade order and live rolling replacement:** Upgrade the PostgreSQL-backed
  Hub first using its two-replica zero-unavailable rolling update, then upgrade
  Spoke Agents per Environment. Forward-only SQL migrations (`internal/pgmigrate`)
  and in-flight lease renewal across a rolling Hub replacement are rehearsed
  against the newest `vX.Y.Z` tag by [`scripts/test-upgrade-data.sh`](../../scripts/test-upgrade-data.sh)
  ([`old-data-upgrade-rehearsal.md`](old-data-upgrade-rehearsal.md)) and
  [`scripts/test-hub-rolling-upgrade.sh`](../../scripts/test-hub-rolling-upgrade.sh)
  ([`hub-rolling-upgrade-rehearsal.md`](hub-rolling-upgrade-rehearsal.md)).
- **Rollback:** Database schema migrations are forward-only; there are no down
  migrations. Roll back a binary or chart release by deploying the previous
  verified image and chart digest (or restoring the pre-upgrade PostgreSQL
  backup via [`backup-and-restore.md`](backup-and-restore.md) if an incompatible
  schema change landed). Never move or overwrite an existing `vX.Y.Z` tag.

