#!/usr/bin/env bash
# Exercises scripts/test-zarf-package-lifecycle.sh without a cluster. The plan
# output is the contract of the lifecycle lane: which artifact each release
# comes from, the signer each is verified against, and what must survive
# removal. This does not run the lane, and nothing here shows that it works.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
lifecycle="${root}/scripts/test-zarf-package-lifecycle.sh"
version="$(tr -d '\n' < "${root}/VERSION")"

fail() {
  echo "$1" >&2
  exit 1
}
has() {
  printf '%s\n' "$1" | grep -qxF -- "$2" || fail "$3"
}

plan="$(env -u LIFECYCLE_VERSION -u LIFECYCLE_PREVIOUS_VERSION -u LIFECYCLE_ARCHITECTURE -u GITHUB_SERVER_URL \
  GITHUB_REPOSITORY=example/conduit "${lifecycle}" --print-plan)"
has "${plan}" "version=${version}" 'the lane must default to VERSION'
has "${plan}" 'verify=always' 'both deployments must verify the signature'
has "${plan}" 'issuer=https://token.actions.githubusercontent.com' 'the signer must be GitHub Actions OIDC'
has "${plan}" "version_source=oci://ghcr.io/example/conduit/packages/platform-runtime:${version}" \
  'release N must come from the OCI registry the publication workflow pushes to'
has "${plan}" "version_identity=https://github.com/example/conduit/.github/workflows/platform-images.yml@refs/tags/v${version}" \
  'release N must be verified against the publication workflow at its own tag'
has "${plan}" 'retained=persistentvolumeclaim/platform-delivery-state' 'the lane must assert the Hub state claim is retained'

override="$(GITHUB_REPOSITORY=example/conduit LIFECYCLE_VERSION=1.2.3 LIFECYCLE_PREVIOUS_VERSION=1.2.2 LIFECYCLE_ARCHITECTURE=arm64 \
  "${lifecycle}" --print-plan)"
has "${override}" 'previous_source=release-asset:zarf-package-platform-runtime-arm64-1.2.2.tar.zst' \
  'release N-1 must come from its GitHub Release asset, for the selected architecture'
has "${override}" 'previous_identity=https://github.com/example/conduit/.github/workflows/platform-images.yml@refs/tags/v1.2.2' \
  'release N-1 must be verified against the publication workflow at its own tag, not at N'
has "${override}" 'version_source=oci://ghcr.io/example/conduit/packages/platform-runtime:1.2.3' 'LIFECYCLE_VERSION must select release N'

if LIFECYCLE_VERSION=latest "${lifecycle}" --print-plan >/dev/null 2>&1; then
  fail 'a non-semantic version must be refused'
fi
if LIFECYCLE_ARCHITECTURE=s390x "${lifecycle}" --print-plan >/dev/null 2>&1; then
  fail 'an unpackaged architecture must be refused'
fi

# The coordinates the lane pulls must be the ones the publication workflow
# pushes and the README tells an operator to verify against.
images="${root}/.github/workflows/platform-images.yml"
grep -qF 'repository="oci://ghcr.io/${GITHUB_REPOSITORY}/packages"' "${images}" ||
  fail 'the publication workflow no longer publishes to the namespace the lifecycle lane pulls from'
grep -qF "platform-images.yml@refs/tags/v" "${root}/deploy/zarf/README.md" ||
  fail 'the README no longer documents the certificate identity the lifecycle lane derives'

echo 'Zarf package lifecycle plan names the release asset, the published package, and each signer'
