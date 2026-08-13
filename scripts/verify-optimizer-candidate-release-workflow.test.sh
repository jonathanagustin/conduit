#!/usr/bin/env bash
set -euo pipefail

repository_root="$(git rev-parse --show-toplevel)"
verifier="$repository_root/scripts/verify-optimizer-candidate-release-workflow.rb"
workflow="$repository_root/.github/workflows/platform-optimizer-candidate-release.yml"
fixture="$(mktemp -d /tmp/platform-optimizer-workflow-test.XXXXXXXX)"

cleanup() {
  case "$fixture" in
    /tmp/platform-optimizer-workflow-test.*) rm -rf -- "$fixture" ;;
  esac
}
trap cleanup EXIT

expect_failure() {
  local name="$1"
  local candidate="$fixture/${name}.yml"
  shift
  cp "$workflow" "$candidate"
  "$@" "$candidate"
  if ruby "$verifier" "$candidate" >"$fixture/${name}.out" 2>&1; then
    printf 'expected workflow mutation to fail: %s\n' "$name" >&2
    exit 1
  fi
}

ruby "$verifier" "$workflow"

add_push_trigger() {
  sed -i '/^  workflow_dispatch:/i\  push:' "$1"
}
remove_environment_gate() {
  sed -i 's/environment: optimizer-candidate-publish/environment: production/' "$1"
}
broaden_root_permissions() {
  sed -i '0,/contents: read/s//packages: write/' "$1"
}
float_checkout_action() {
  sed -i '0,/actions\/checkout@[0-9a-f]\{40\}/s//actions\/checkout@v6/' "$1"
}
relabel_candidate() {
  sed -i 's/admission: "UNADMITTED"/admission: "ENABLED"/g' "$1"
}
remove_revision_binding() {
  sed -i 's/\[\[ "${REVISION}" == "${GITHUB_SHA}" \]\]/true/' "$1"
}
add_native_write_permission() {
  sed -i '/^  native-candidate:/,/^    timeout-minutes:/{s/^      contents: read$/      contents: read\n      packages: write/;}' "$1"
}
remove_source_date_epoch() {
  sed -i '/--build-arg "SOURCE_DATE_EPOCH=${source_date_epoch}"/d' "$1"
}

expect_failure automatic_trigger add_push_trigger
expect_failure foreign_environment remove_environment_gate
expect_failure broad_root_permission broaden_root_permissions
expect_failure floating_action float_checkout_action
expect_failure enabled_relabel relabel_candidate
expect_failure missing_revision_join remove_revision_binding
expect_failure native_write_permission add_native_write_permission
expect_failure missing_source_date_epoch remove_source_date_epoch

printf 'optimizer candidate release workflow fixtures: pass\n'
