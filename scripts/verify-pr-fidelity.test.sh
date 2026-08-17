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
  '      - name: Publish gate summary' \
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
expect_failure unfilled_marker "${valid/Generate the pinned public API transport interface./TODO}"

# Every upper-case marker is still refused in prose. Listed one by one because
# the alternation is the part most likely to be edited down to whichever one
# somebody was thinking about.
expect_failure marker_tbd "${valid/Generate the pinned public API transport interface./TBD}"
expect_failure marker_placeholder "${valid/Generate the pinned public API transport interface./PLACEHOLDER}"
expect_failure marker_fill_me "${valid/Generate the pinned public API transport interface./FILL ME}"
expect_failure marker_fill_hyphen "${valid/Generate the pinned public API transport interface./FILL-ME}"
# A marker with prose around it is still a marker; matching must not depend on
# the line containing nothing else.
expect_failure marker_in_a_sentence "${valid/Generate the pinned public API transport interface./The response shape is TODO once the capture lands.}"

expect_success() {
  local name="$1" body="$2"
  printf '%s' "$body" | "$verifier" >"${TMPDIR:-/tmp}/conduit-pr-fidelity-${name}.out" 2>&1 || {
    printf 'expected tenant-fidelity verification to pass: %s\n' "$name" >&2
    cat "${TMPDIR:-/tmp}/conduit-pr-fidelity-${name}.out" >&2
    exit 1
  }
}

# The two cases that rejected correct work on #137, which is why this gate is
# case-sensitive and code-aware. The first is an ordinary English word; the
# second is this list quoted while explaining the first.
expect_success prose_noun "${valid/Generate the pinned public API transport interface./Apollo publishes the stand-in placeholder value 123, so no locator format is established.}"
expect_success marker_in_code_span "${valid/Generate the pinned public API transport interface./The gate greps for \`TBD|TODO|PLACEHOLDER\` to catch unfilled markers.}"
expect_success lowercase_prose "${valid/Generate the pinned public API transport interface./There is a todo list in the design note, and a tbd column in the table.}"

fenced="${valid/Generate the pinned public API transport interface./Explaining the template:

\`\`\`markdown
## Outcome
TODO
\`\`\`

which is what an unfilled body looks like.}"
expect_success marker_in_fenced_block "$fenced"

echo 'tenant-fidelity verifier tests passed'
