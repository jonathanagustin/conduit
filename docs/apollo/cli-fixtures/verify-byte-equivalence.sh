#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 /absolute/path/to/captured/<version> /absolute/path/to/candidate-cli" >&2
  exit 2
fi

reference="$1"
candidate="$2"
if [[ "${reference}" != /* || ! -d "${reference}" ]]; then
  echo "reference must be an absolute native capture directory" >&2
  exit 2
fi
if [[ "${candidate}" != /* || ! -x "${candidate}" ]]; then
  echo "candidate must be an absolute executable path" >&2
  exit 2
fi
for required in generated/input-chart.tgz generated/manifest.yml generated/product.config.tgz; do
  if [[ ! -f "${reference}/${required}" ]]; then
    echo "native capture is incomplete: missing ${required}" >&2
    exit 2
  fi
done

scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
mkdir -p "${scratch}/home" "${scratch}/manifest" "${scratch}/archive"
run_candidate() {
  env HOME="${scratch}/home" "${candidate}" "$@"
}
common_args=(
  --chart-path "${reference}/generated/input-chart.tgz"
  --repository-url "oci://registry.example/charts/conformance-fixture"
  --maven-coordinate "com.example.conformance:conformance-fixture:1.2.3"
)
run_candidate product-release helm-chart init "${common_args[@]}" --output-dir "${scratch}/manifest"
run_candidate product-release helm-chart init --as-tgz "${common_args[@]}" --output-dir "${scratch}/archive"
candidate_archive="$(find "${scratch}/archive" -maxdepth 1 -name '*.config.tgz' -print -quit)"
test -n "${candidate_archive}"

status=0
for pair in \
  "generated/manifest.yml:${scratch}/manifest/manifest.yml" \
  "generated/product.config.tgz:${candidate_archive}"; do
  reference_relative="${pair%%:*}"
  candidate_path="${pair#*:}"
  if ! cmp -s "${reference}/${reference_relative}" "${candidate_path}"; then
    echo "BYTE_MISMATCH ${reference_relative}" >&2
    sha256sum "${reference}/${reference_relative}" "${candidate_path}" >&2
    status=1
  else
    echo "BYTE_EQUAL ${reference_relative}"
  fi
done
exit "${status}"
