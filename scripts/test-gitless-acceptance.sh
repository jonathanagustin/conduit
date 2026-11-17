#!/usr/bin/env bash
# Runs the Gitless GitOps loop end to end, with nothing faked.
#
# Every other test in internal/gitless substitutes something: the domain tests
# construct an attestation, the adapter tests use httptest and a stub cosign,
# and the equivalence test drives both convergers against doubles. None of that
# answers whether the whole thing works against a registry, a signature, a
# policy and a cluster that do not know they are being tested.
#
# This does. It resolves a real channel on a real registry, verifies a real
# keyless signature with the real cosign binary, admits it through the real
# domain rule, records it with a real fsynced append, and applies real Flux
# manifests to a real API server -- then does it again under a policy naming a
# different publisher and requires nothing to reach the cluster.
#
# Refuses skipped or missing acceptance bodies. It needs network: the artifact
# it verifies is public and
# signed by somebody else on purpose, because a policy that matched the thing it
# constrains would accept anything.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

echo "gitless acceptance: running the real path"
events="$(mktemp)"
trap 'rm -f "${events}"' EXIT
if ! GITLESS_ACCEPTANCE=1 go test ./internal/gitless/ -run '^TestAcceptance' -count=1 -json >"${events}"; then
  cat "${events}" >&2
  echo "gitless acceptance: FAILED" >&2
  exit 1
fi
"${repo_root}/scripts/verify-gitless-acceptance-events.sh" "${events}"
echo "gitless acceptance: complete"
