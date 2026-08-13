#!/usr/bin/env bash
# The VERSION file and the release tag must name the same release.
#
# There are two version sources in this repository and nothing reconciled them.
# docs/RELEASE.md declares the scheme as SemVer derived from git tags by
# com.palantir.gradle.gitversion, while platform-images.yml reads this VERSION
# file in six places to stamp every published image, OCI artifact and chart.
#
# They drifted: VERSION sat at 0.14.0 while the tags reached v0.16.2, so a
# release build would have published 0.14.0-labelled artifacts from a v0.16.2
# tree. Mirroring by digest into a downstream registry would then have copied a
# correctly signed and wrongly labelled artifact, which is worse than an
# unsigned one because the provenance chain verifies.
#
# The tag is authoritative, per RELEASE.md. This asserts the file agrees with
# it. Comparing against the most recent tag rather than the exact commit is
# deliberate: commits land after a tag and before the next one, and those are
# still that release's line.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version_file="${repo_root}/VERSION"

if [[ ! -f "${version_file}" ]]; then
  echo "no VERSION file at ${version_file}" >&2
  exit 1
fi

version="$(tr -d '[:space:]' < "${version_file}")"

# A shallow clone has no tags, and a fork may have none of ours. Neither is a
# reason to fail a build; the check simply cannot run.
if ! tag="$(git -C "${repo_root}" describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null)"; then
  echo "version/tag agreement: skipped, no reachable v* tag"
  exit 0
fi

tagged="${tag#v}"

# Ahead is a release being prepared; behind is the drift this exists for.
#
# Requiring equality looked right and made the ordinary flow impossible: the
# commit that bumps VERSION cannot also carry the tag that confirms it, so the
# gate refused the very pull request that fixes it — and told the author to go
# backwards. What actually went wrong was VERSION sitting at 0.14.0 while the
# tags reached 0.16.2, and that is only ever the behind case.
sortable() { printf '%s\n' "$1" | awk -F. '{ printf "%010d%010d%010d\n", $1, $2, $3 }'; }

if [[ "$(sortable "${version}")" < "$(sortable "${tagged}")" ]]; then
  cat >&2 <<EOF
VERSION says ${version}, and the most recent tag is already ${tagged}.

Every image, OCI artifact and chart is stamped from VERSION, so a release build
would publish them under a version this tree left behind. Bump VERSION to at
least ${tagged}.
EOF
  exit 1
fi

if [[ "${version}" == "${tagged}" ]]; then
  echo "version/tag agreement: VERSION ${version} matches ${tag}"
else
  echo "version/tag agreement: VERSION ${version} is ahead of ${tag}, which is a release in preparation"
fi
