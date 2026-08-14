#!/usr/bin/env bash
set -euo pipefail

# The Codespaces workspace persists across stop/start; /tmp does not. Cache
# symlinks therefore survive while their targets disappear. Run once per
# volatile-disk lifetime, retry after a failed repair, and do not touch source,
# evidence, credentials, or any non-symlink path.
repo_root="${CONDUIT_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
state_root="${CONDUIT_BOOT_STATE_ROOT:-/tmp/conduit-self-heal}"
complete="${state_root}/complete"
lock="${state_root}/lock"

mkdir -p "${state_root}"
[[ -f "${complete}" ]] && exit 0
if ! mkdir "${lock}" 2>/dev/null; then
	exit 0
fi
cleanup() { rmdir "${lock}" 2>/dev/null || true; }
trap cleanup EXIT INT TERM

"${repo_root}/scripts/repair-volatile-cache-links.sh"
printf '%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"${complete}"
