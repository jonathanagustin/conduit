#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
retry="${repo_root}/scripts/retry-keyless-signing.sh"
test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT

cat > "${test_dir}/eventual-success.sh" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
count=0
if [[ -f "$1" ]]; then count="$(cat "$1")"; fi
count=$((count + 1))
printf '%s\n' "${count}" > "$1"
(( count >= 3 ))
SCRIPT
chmod 0755 "${test_dir}/eventual-success.sh"

KEYLESS_SIGN_MAX_ATTEMPTS=4 KEYLESS_SIGN_INITIAL_DELAY_SECONDS=0 \
  "${retry}" "${test_dir}/eventual-success.sh" "${test_dir}/success-count"
test "$(cat "${test_dir}/success-count")" = 3

cat > "${test_dir}/always-fail.sh" <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
count=0
if [[ -f "$1" ]]; then count="$(cat "$1")"; fi
printf '%s\n' "$((count + 1))" > "$1"
exit 23
SCRIPT
chmod 0755 "${test_dir}/always-fail.sh"

set +e
KEYLESS_SIGN_MAX_ATTEMPTS=2 KEYLESS_SIGN_INITIAL_DELAY_SECONDS=0 \
  "${retry}" "${test_dir}/always-fail.sh" "${test_dir}/failure-count"
status=$?
set -e
test "${status}" = 23
test "$(cat "${test_dir}/failure-count")" = 2

if KEYLESS_SIGN_MAX_ATTEMPTS=0 "${retry}" true; then
  echo "invalid retry configuration unexpectedly succeeded" >&2
  exit 1
fi

echo "Keyless signing retry behavior verified"
