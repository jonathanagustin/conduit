#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../../.." && pwd)"
fixture_root="${repo_root}/docs/apollo/cli-fixtures"
cli="${APOLLO_CLI:-}"

if [[ -z "${cli}" || ! -x "${cli}" || "${cli}" != /* ]]; then
  echo "APOLLO_CLI must name an executable tenant-downloaded Apollo CLI" >&2
  exit 2
fi
command -v helm >/dev/null

scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
mkdir -p "${scratch}/home" "${scratch}/chart" "${scratch}/generated"

run_cli() {
  env HOME="${scratch}/home" "${cli}" "$@"
}

version="$(run_cli version --output json 2>/dev/null | tr -d '\n' || true)"
if [[ -z "${version}" ]]; then
  version="$(run_cli --version 2>/dev/null | tr -d '\n' || true)"
fi
if [[ -z "${version}" ]]; then
  echo "Unable to determine Apollo CLI version" >&2
  exit 1
fi
safe_version="$(printf '%s' "${version}" | sed -E 's/[^0-9A-Za-z._-]+/_/g')"
capture="${fixture_root}/captured/${safe_version}"
mkdir -p "${capture}/help" "${capture}/generated"
printf '%s\n' "${version}" >"${capture}/version.txt"
sha256sum "${cli}" | awk '{print $1}' >"${capture}/binary.sha256"

commands=(
  ""
  "artifact-registry"
  "changerequest"
  "cve"
  "entity"
  "environment"
  "helm-chart"
  "module"
  "product"
  "product-release"
  "product-release helm-chart"
  "product-release helm-chart init"
  "product-release verify"
  "profile"
  "release-channel"
  "terminal"
)
for command_path in "${commands[@]}"; do
  filename="root"
  if [[ -n "${command_path}" ]]; then
    filename="${command_path// /_}"
  fi
  # shellcheck disable=SC2086
  run_cli ${command_path} --help >"${capture}/help/${filename}.txt" 2>&1
done

helm package "${fixture_root}/input-chart" --destination "${scratch}/chart" >/dev/null
chart="${scratch}/chart/conformance-fixture-1.2.3.tgz"
cp "${chart}" "${capture}/generated/input-chart.tgz"
run_cli product-release helm-chart init \
  --chart-path "${chart}" \
  --repository-url "oci://registry.example/charts/conformance-fixture" \
  --maven-coordinate "com.example.conformance:conformance-fixture:1.2.3" \
  --output-dir "${scratch}/generated"

cp "${scratch}/generated/manifest.yml" "${capture}/generated/manifest.yml"
run_cli product-release verify \
  --manifest "${scratch}/generated/manifest.yml" \
  --print-manifest >"${capture}/generated/verify.txt" 2>&1

capture_invalid_version() {
  local label="$1"
  local version="$2"
  local invalid_manifest="${scratch}/generated/invalid-${label}.yml"
  sed -E "s#^product-version:.*#product-version: ${version}#" \
    "${scratch}/generated/manifest.yml" >"${invalid_manifest}"
  set +e
  run_cli product-release verify --manifest "${invalid_manifest}" \
    >"${capture}/generated/invalid-${label}.txt" 2>&1
  local status=$?
  set -e
  if [[ ${status} -eq 0 ]]; then
    echo "Apollo CLI unexpectedly accepted invalid Product version ${version}" >&2
    exit 1
  fi
  printf '%s\n' "${status}" >"${capture}/generated/invalid-${label}.exit-code"
}

capture_invalid_version "incomplete" "1.2"
capture_invalid_version "component-overflow" "2147483648.0.0"

rm -f "${scratch}/generated/"*.config.tgz
run_cli product-release helm-chart init \
  --as-tgz \
  --chart-path "${chart}" \
  --repository-url "oci://registry.example/charts/conformance-fixture" \
  --maven-coordinate "com.example.conformance:conformance-fixture:1.2.3" \
  --output-dir "${scratch}/generated"
archive="$(find "${scratch}/generated" -maxdepth 1 -name '*.config.tgz' -print -quit)"
test -n "${archive}"
cp "${archive}" "${capture}/generated/product.config.tgz"
tar -tzf "${archive}" | LC_ALL=C sort >"${capture}/generated/archive-files.txt"
tar -xOzf "${archive}" '*/deployment/manifest.yml' >"${capture}/generated/archive-manifest.yml"
(cd "${capture}" && {
  find generated help -type f -print0
  printf '%s\0' version.txt binary.sha256
} | LC_ALL=C sort -z | xargs -0 sha256sum) >"${capture}/SHA256SUMS"

if rg -i 'bearer|client.?secret|palantirfoundry\.com|palantircloud\.com' "${capture}"; then
  echo "Captured output contains credential or tenant markers; remove the capture and review manually" >&2
  exit 1
fi
echo "Captured credential-isolated Apollo CLI fixtures in ${capture}"
