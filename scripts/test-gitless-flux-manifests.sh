#!/usr/bin/env bash
# Validates the manifests internal/gitless/adapters/fluxconverge renders against
# a real Flux API server.
#
# Why this exists separately from the Go tests: a structural test passes on a
# document Flux would reject. Marshalling a map to YAML with plausible field
# names proves nothing about whether `source.toolkit.fluxcd.io/v1` actually has
# those fields. Only an API server with the CRDs installed can answer that, so
# this dry-runs the committed golden against one.
#
# And why the Go tests are still the ones that matter: this script CANNOT catch
# a policy regression. The Flux CRDs make `spec.verify` optional and
# `spec.serviceAccountName` optional, so an API server happily accepts a
# manifest that has dropped Cosign verification and reconciles with the
# controller's cluster-wide identity. That is asserted below as a known limit
# rather than left for someone to assume otherwise.
#
# Requires a cluster with Flux CRDs. Skips cleanly without one, because most
# environments that run this repository's gates have neither.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
golden="${repo_root}/internal/gitless/adapters/fluxconverge/testdata/rendered.golden.yaml"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

if ! command -v kubectl >/dev/null 2>&1; then
  echo "gitless Flux manifests: SKIP (no kubectl)"
  exit 0
fi
if ! kubectl get crd ocirepositories.source.toolkit.fluxcd.io >/dev/null 2>&1; then
  echo "gitless Flux manifests: SKIP (no Flux CRDs in the reachable cluster)"
  exit 0
fi

apply() { kubectl apply --dry-run=server -f "$1" 2>&1; }

echo "gitless Flux manifests: the rendered golden must be valid Flux API"
if ! apply "$golden" >"$work/accepted" 2>&1; then
  echo "FAIL: a real Flux API server rejected the rendered manifests:" >&2
  cat "$work/accepted" >&2
  exit 1
fi
grep -q "ocirepository.*created" "$work/accepted" || { echo "FAIL: no OCIRepository was accepted" >&2; exit 1; }
grep -q "kustomization.*created" "$work/accepted" || { echo "FAIL: no Kustomization was accepted" >&2; exit 1; }
echo "  accepted: OCIRepository and Kustomization"

# A check that only ever passes proves the cluster is up, not that the check
# works. These are shape errors the API server genuinely catches.
expect_reject() {
  local name="$1" edit="$2"
  sed "$edit" "$golden" >"$work/mutant.yaml"
  # The output is captured before it is matched. Piping kubectl into grep under
  # `set -o pipefail` fails the pipeline on kubectl's own non-zero exit, which
  # is exactly the exit a rejection produces -- so the check would report every
  # rejection as an acceptance.
  local output
  output="$(apply "$work/mutant.yaml" || true)"
  if grep -qiE "error|invalid" <<<"$output"; then
    printf '  rejected: %s\n' "$name"
  else
    echo "FAIL: the API server accepted ${name}" >&2
    exit 1
  fi
}

echo "gitless Flux manifests: and the API server must refuse these"
expect_reject "a field the Flux API does not define" 's|  interval: 5m|  interval: 5m\n  bogusField: x|'
expect_reject "a source with no url" '/  url: oci/d'
expect_reject "a digest that is not a string" 's|digest: sha256:aa11.*|digest: 12345|'

# The known limit, asserted rather than assumed. If Flux ever makes verification
# mandatory this line fails, and that is worth knowing -- it would mean the Go
# test guarding it could be simplified.
echo "gitless Flux manifests: and the limit this check cannot cover"
sed '/  verify:/,+4d' "$golden" >"$work/unverified.yaml"
unverified_output="$(apply "$work/unverified.yaml" || true)"
if grep -qiE "error|invalid" <<<"$unverified_output"; then
  echo "NOTE: Flux now rejects a source with no verify block. Update this script and" >&2
  echo "      internal/gitless/adapters/fluxconverge/flux_test.go, which currently carries that duty." >&2
  exit 1
fi
echo "  confirmed: an API server ACCEPTS a source with Cosign verification removed."
echo "             Policy is guarded by the Go tests, not by this script."

echo "gitless Flux manifests: all cases behaved"
