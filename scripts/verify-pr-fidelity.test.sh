#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="$root/scripts/verify-pr-fidelity.sh"
workflow="$root/.github/workflows/platform-ci.yml"

for required in \
  '  statuses: write' \
  '      - name: Publish pending Trunk status' \
  "            -f state=pending" \
  "            -f context='Trunk gate'" \
  '      - name: Publish final Trunk status' \
  '        if: ${{ always() }}' \
  "          TRUNK_STATE: \${{ job.status == 'success' && 'success' || 'failure' }}"; do
  grep -Fq -- "$required" "$workflow" || {
    printf 'manual Trunk status bridge is incomplete: %s\n' "$required" >&2
    exit 1
  }
done

valid='## Outcome

Generate the pinned public API transport interface.

## Tenant fidelity

| Claim or behavior | Class: Public contract / Tenant observation / Corroborating / Conduit extension / Unknown | Immutable source or sanitized evidence | Scope not established |
| --- | --- | --- | --- |
| Public operation shape | Public contract | docs/apollo/snapshots/example/openapi.yml at sha256:abc | Private Hub behavior |

### Deliberate divergences

None; the adapter exposes only the pinned profile.
'

printf '%s' "$valid" | "$verifier" >/dev/null

expect_failure() {
  local name="$1" body="$2"
  if printf '%s' "$body" | "$verifier" >"${TMPDIR:-/tmp}/conduit-pr-fidelity-${name}.out" 2>&1; then
    printf 'expected tenant-fidelity verification to fail: %s\n' "$name" >&2
    exit 1
  fi
}

expect_failure missing_row "${valid/Public operation shape | Public contract | docs\/apollo\/snapshots\/example\/openapi.yml at sha256:abc | Private Hub behavior/ | Public contract | | Private Hub behavior}"
expect_failure empty_divergence "${valid/None; the adapter exposes only the pinned profile./}"
expect_failure placeholder "${valid/Generate the pinned public API transport interface./TODO}"

echo 'tenant-fidelity verifier tests passed'
