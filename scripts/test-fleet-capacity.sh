#!/usr/bin/env bash
# Repeatable, opt-in local load/soak. Writes no deployment and uses no tenant data.
set -euo pipefail
umask 077
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
commit="$(git rev-parse HEAD)"
tree="$(git rev-parse 'HEAD^{tree}')"
[[ -z "$(git status --porcelain --untracked-files=normal)" ]] || {
  echo 'capacity measurement requires a committed, clean checkout' >&2
  exit 1
}
report_dir="${1:-$(mktemp -d "${TMPDIR:-/tmp}/conduit-capacity.XXXXXXXX")}"
mkdir -p "${report_dir}"
[[ ! -e "${report_dir}/report.json" && ! -e "${report_dir}/receipt.json" ]] || {
  echo 'capacity report destination already contains a measurement' >&2
  exit 1
}
report_dir="$(cd "${report_dir}" && pwd)"
CONDUIT_CAPACITY_RUN=1 CONDUIT_CAPACITY_COMMIT="${commit}" \
  CONDUIT_CAPACITY_REPORT="${report_dir}/report.json" \
  go test ./internal/hub/composition -run '^TestFleetCapacity$' -count=1 -v \
    -timeout "${CONDUIT_CAPACITY_GO_TIMEOUT:-30m}" 2>&1 | tee "${report_dir}/go-test.log"
[[ "$(git rev-parse HEAD)" == "${commit}" && -z "$(git status --porcelain --untracked-files=normal)" ]] || {
  echo 'checkout changed during capacity measurement; no success receipt written' >&2
  exit 1
}
hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}'
  else echo 'sha256sum or shasum is required for measurement evidence' >&2; return 1
  fi
}
report_sha="$(hash_file "${report_dir}/report.json")"
log_sha="$(hash_file "${report_dir}/go-test.log")"
printf '{"schema_version":1,"kind":"CONDUIT_CAPACITY","result":"PASS","commit":"%s","tree":"%s","report_sha256":"%s","log_sha256":"%s","production_capacity_established":false}\n' \
  "${commit}" "${tree}" "${report_sha}" "${log_sha}" > "${report_dir}/receipt.json"
printf 'Capacity evidence: %s\n' "${report_dir}"
