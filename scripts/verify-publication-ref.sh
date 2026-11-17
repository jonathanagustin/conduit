#!/usr/bin/env bash
# Refuses to publish from anything that is not the release tag.
#
# platform-images.yml is dispatch-only, and a dispatch can name any ref. From a
# branch it did not fail: the image push is gated on
# `github.event_name != 'pull_request'`, which is always true on a workflow that
# has no pull_request trigger, while the Flux runtime source, the Helm charts,
# the Zarf packages, the CLI archives and the GitHub release are each gated on
# `startsWith(github.ref, 'refs/tags/v')`. So a branch dispatch pushed
# `<component>-sha-<sha>` images, skipped every other step, and reported success
# for a fraction of a release (#196).
#
# That is the failure class this repository refuses elsewhere -- a green run
# that verified less than it appears to -- and it is worse here than a red one,
# because the fraction that did publish is real, signed, and in the registry.
#
# This lives in the publication workflow rather than in the release workflow
# that calls it, because the hole is reachable without the caller: anyone who
# can dispatch platform-images.yml can dispatch it from the Actions UI against
# main. A guard in the caller only guards the path that goes through the caller.
set -euo pipefail

fail() {
  printf '%s\n' "$*" >&2
  exit 1
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ref="${GITHUB_REF:-}"
ref_name="${GITHUB_REF_NAME:-}"
version="$(tr -d '[:space:]' < "${repo_root}/VERSION")"

if [[ ! "${ref}" =~ ^refs/tags/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  cat >&2 <<EOF
Publication was dispatched against '${ref:-an empty ref}', which is not a vX.Y.Z release tag.

Nothing about this run would have failed: it would have pushed
'<component>-sha-<commit>' images and skipped the runtime source, the charts,
the offline-bundle packages, the CLI and the release, then reported success having
published a fraction of a release.

Cut releases through .github/workflows/platform-release.yml, which verifies the
version, requires a green Trunk gate for the commit, creates the tag, and
dispatches this workflow against it.
EOF
  exit 1
fi

if [[ "${ref_name}" != "v${version}" ]]; then
  cat >&2 <<EOF
Publication was dispatched against ${ref_name}, and VERSION at this commit says ${version}.

Every artifact is stamped from VERSION while the tag names the release, so this
run would publish ${version}-labelled artifacts under ${ref_name} -- signed, and
wrongly labelled.
EOF
  exit 1
fi

# This workflow is directly dispatchable: the release caller's check alone
# cannot prevent publication of an unqualified tag.
RELEASE_COMMIT="${GITHUB_SHA:-}" "${repo_root}/scripts/verify-cluster-qualification.sh"

echo "publication ref: ${ref_name} is a release tag and agrees with VERSION ${version}"
