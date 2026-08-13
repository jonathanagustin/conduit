#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <git-commit> <output-json>" >&2
  exit 2
fi

commit="$1"
output="$2"
repo_root="$(git rev-parse --show-toplevel)"
commit="$(git -C "$repo_root" rev-parse --verify "${commit}^{commit}")"
tree="$(git -C "$repo_root" rev-parse "${commit}^{tree}")"

archive_digest="$({
  git -C "$repo_root" archive --format=tar "$commit"
} | sha256sum | awk '{print $1}')"

# Scan only credential forms that should never occur in a sanitized source
# snapshot. Keep the expression versioned and content-addressed in the receipt;
# do not print matching content if the gate fails.
secret_pattern='(crsr_[[:alnum:]_-]{32,}|github_pat_[[:alnum:]_]{40,}|gh[pousr]_[[:alnum:]]{30,}|AKIA[[:alnum:]]{16}|-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----)'
scan_revision="sha256:$(printf '%s' "$secret_pattern" | sha256sum | awk '{print $1}')"
mapfile -t matched_paths < <(
  git -C "$repo_root" grep -I -l -E "$secret_pattern" "$commit" -- . \
    ':(exclude)scripts/build-evaluator-snapshot.sh' || true
)
if (( ${#matched_paths[@]} > 0 )); then
  printf 'credential-shaped content found in snapshot path: %s\n' "${matched_paths[@]}" >&2
  exit 1
fi

mkdir -p "$(dirname "$output")"
jq -n \
  --arg apiVersion 'optimization.platform.example/evaluator-snapshot/v1alpha1' \
  --arg commit "$commit" \
  --arg tree "$tree" \
  --arg archiveSha256 "$archive_digest" \
  --arg snapshotId "snap_${archive_digest}" \
  --arg secretScanRevision "$scan_revision" \
  '{
    apiVersion: $apiVersion,
    kind: "EvaluatorSnapshot",
    repository: "jonathanagustin/conduit",
    path: ".",
    commit: $commit,
    tree: $tree,
    archiveSha256: $archiveSha256,
    snapshotId: $snapshotId,
    secretScanRevision: $secretScanRevision,
    secretMatches: 0
  }' > "$output"

jq -e '.snapshotId == ("snap_" + .archiveSha256) and .secretMatches == 0' "$output" >/dev/null
echo "snapshot=$(jq -r .snapshotId "$output") commit=$commit"
