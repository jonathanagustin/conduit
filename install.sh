#!/usr/bin/env bash
# Run scripts/install.sh. Source this file to activate mise in the current shell.

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
installer="${root}/scripts/install.sh"

if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
	# shellcheck source=scripts/install.sh
	source "${installer}" || return $?
else
	set -euo pipefail
	exec "${installer}" "$@"
fi
