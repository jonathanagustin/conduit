#!/usr/bin/env bash
set -euo pipefail

if [[ "${CODESPACES:-false}" != "true" && "${CONDUIT_INSTALL_SELF_HEAL:-0}" != "1" ]]; then
	exit 0
fi

repo_root="${CONDUIT_REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
bashrc="${CONDUIT_BASHRC:-${HOME}/.bashrc}"
begin="# >>> conduit codespace self-heal >>>"
end="# <<< conduit codespace self-heal <<<"
temporary="$(mktemp)"
trap 'rm -f "${temporary}"' EXIT

mkdir -p "$(dirname "${bashrc}")"
touch "${bashrc}"
awk -v begin="${begin}" -v end="${end}" '
	$0 == begin { skipping=1; next }
	$0 == end { skipping=0; next }
	!skipping { print }
' "${bashrc}" >"${temporary}"
cat >>"${temporary}" <<EOF
${begin}
if [ -x '${repo_root}/scripts/codespace-self-heal.sh' ]; then
  '${repo_root}/scripts/codespace-self-heal.sh' >>/tmp/conduit-self-heal.log 2>&1 &
fi
${end}
EOF
mv "${temporary}" "${bashrc}"
trap - EXIT
