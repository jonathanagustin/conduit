#!/usr/bin/env bash
set -euo pipefail

# Codespaces may offload rebuildable caches through absolute symlinks into
# /tmp. A restart preserves the link but deletes its target; mkdir then reports
# "file exists" against the dangling link. Recreate only explicitly named
# directory targets under the validated volatile root. This never creates or
# replaces a link and never touches repository data.
volatile_root="${PLATFORM_VOLATILE_CACHE_ROOT:-/tmp/workspace-offload}"
mise_volatile_root="${PLATFORM_MISE_VOLATILE_CACHE_ROOT:-/tmp/codespace-offload}"

repair_directory_link() {
	local link="$1" approved_root="${2:-${volatile_root}}" target
	[[ -L "${link}" ]] || return 0
	target="$(readlink "${link}")"
	[[ "${target}" = /* ]] || return 0
	case "${target}" in
		"${approved_root}"/*) ;;
		*) echo "refusing cache link outside ${approved_root}: ${link} -> ${target}" >&2; return 1 ;;
	esac
	if [[ ! -e "${target}" ]]; then
		mkdir -p "${target}"
	fi
	[[ -d "${target}" ]] || { echo "cache target is not a directory: ${target}" >&2; return 1; }
}

platform_user_home="${HOME}"
repair_directory_link "${PLATFORM_GO_MOD_LINK:-/go/pkg/mod}"
repair_directory_link "${PLATFORM_GO_BUILD_LINK:-${platform_user_home}/.cache/go-build}"
repair_directory_link "${PLATFORM_PLAYWRIGHT_LINK:-${platform_user_home}/.cache/ms-playwright}"
repair_directory_link "${PLATFORM_CURSOR_VERSIONS_LINK:-${platform_user_home}/.local/share/cursor-agent/versions}"
repair_directory_link "${PLATFORM_MISE_DATA_LINK:-${platform_user_home}/.local/share/mise}" "${mise_volatile_root}"
