#!/usr/bin/env bash
# Install the pinned mise release, install the locked toolchain, and report
# host prerequisites for the local demo. Re-running skips completed work.
# Source this file to activate mise in the current shell.

sourced=0
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
	sourced=1
else
	set -euo pipefail
fi

# Official release assets for this exact mise version. The checksums are the
# SHA-256 entries from that release. The archive or executable is downloaded
# and checked before it is installed. The installer script from the same
# release is not executed.
mise_version="2026.8.3"

mise_asset_sha256() {
	case "$1" in
	mise-v2026.8.3-linux-x64.tar.gz)
		printf '%s\n' '8aaf21cc4b36681e90a96e9cdf13e5d7511e9773733f741b1a5f7756ba53b5fc'
		;;
	mise-v2026.8.3-linux-x64-musl.tar.gz)
		printf '%s\n' 'd664237d85ef20c53f979476c782891b275f3663a2b6c6193b1c6356869312a5'
		;;
	mise-v2026.8.3-linux-arm64.tar.gz)
		printf '%s\n' '8d0c6142607d814279de0e06f53c9e896b5d267bbced9ee6e2d9e1547fccca8f'
		;;
	mise-v2026.8.3-linux-arm64-musl.tar.gz)
		printf '%s\n' '819ddd5839dc8d4c8669d4a131b72972d9fe97faae43843fb2cf3e752e2d30d9'
		;;
	mise-v2026.8.3-linux-armv7.tar.gz)
		printf '%s\n' '0b9f93b634e01c37b982e687915749c01265b9a084ce115e6a2b1b9c95c4e9d3'
		;;
	mise-v2026.8.3-linux-armv7-musl.tar.gz)
		printf '%s\n' 'd90e086507d724b6a0b06bca1a39e03642367e365270648f999b76b11f37c352'
		;;
	mise-v2026.8.3-macos-x64.tar.gz)
		printf '%s\n' '15310f580fce8b3dad93e5e1a916592651dfdb7fa9a6904cb60ee52e5d8cbecd'
		;;
	mise-v2026.8.3-macos-arm64.tar.gz)
		printf '%s\n' '46f2ff77244fd7caa9602fdb190e5db3da3d60ed5d1b579512aad942303a477a'
		;;
	mise-v2026.8.3-windows-x64.exe)
		printf '%s\n' 'b09bfe160ffa33236f03547e6e0ef2bd937fcd7556b1feb5c2d227c174ef2a22'
		;;
	mise-v2026.8.3-windows-arm64.exe)
		printf '%s\n' 'c6a02022bf20d44f06b631683dc63eea7736ab6beabb4b5504075bdc0de55687'
		;;
	*)
		return 1
		;;
	esac
}

detect_platform() {
	local kernel machine libc_line
	kernel="$(uname -s)"
	machine="$(uname -m)"
	platform_family=""
	platform_arch=""
	platform_libc=""
	platform_ext=""
	platform_id=""
	platform_label="${kernel}"
	platform_machine="${machine}"

	case "${kernel}" in
	Linux)
		platform_family="linux"
		platform_ext="tar.gz"
		platform_label="Linux"
		if [[ -n "${WSL_DISTRO_NAME:-}${WSL_INTEROP:-}" ]] || grep -qi microsoft /proc/version 2>/dev/null; then
			platform_label="WSL Linux"
		fi
		if [[ "${MISE_INSTALL_MUSL:-}" == "1" || "${MISE_INSTALL_MUSL:-}" == "true" ]]; then
			platform_libc="-musl"
		elif command -v ldd >/dev/null 2>&1; then
			libc_line="$(ldd /bin/ls 2>/dev/null || true)"
			if [[ "${libc_line}" == *musl* ]]; then
				platform_libc="-musl"
			fi
		fi
		;;
	Darwin)
		platform_family="macos"
		platform_ext="tar.gz"
		platform_label="macOS"
		;;
	MINGW* | MSYS* | CYGWIN*)
		platform_family="windows"
		platform_ext="exe"
		platform_label="Windows Git Bash"
		;;
	*)
		platform_label="${kernel}"
		;;
	esac

	case "${machine}" in
	x86_64 | amd64) platform_arch="x64" ;;
	aarch64 | arm64) platform_arch="arm64" ;;
	armv7l | armv7) platform_arch="armv7" ;;
	*) platform_arch="" ;;
	esac

	if [[ -n "${platform_family}" && -n "${platform_arch}" ]]; then
		platform_id="${platform_family}-${platform_arch}${platform_libc}"
	fi
}

mise_version_matches() {
	local bin="$1"
	[[ -n "${bin}" && -x "${bin}" ]] || return 1
	"${bin}" --version 2>/dev/null | awk -v want="${mise_version}" 'NR == 1 {
		for (i = 1; i <= NF; i++) {
			token = $i
			sub(/^v/, "", token)
			if (token == want) found = 1
		}
	}
	END { exit(found ? 0 : 1) }'
}

sha256_check() {
	local file="$1" expected="$2" checksums status
	checksums="$(mktemp)"
	printf '%s  %s\n' "${expected}" "$(basename "${file}")" >"${checksums}"
	status=0
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$(dirname "${file}")" && sha256sum --check "${checksums}") || status=$?
	elif command -v shasum >/dev/null 2>&1; then
		(cd "$(dirname "${file}")" && shasum -a 256 --check "${checksums}") || status=$?
	else
		echo "Need sha256sum or shasum to verify the mise download." >&2
		status=1
	fi
	rm -f "${checksums}"
	return "${status}"
}

problem() {
	printf 'Missing: %s\n' "$*"
	problems=$((problems + 1))
}

install_mise_asset() {
	local asset url expected bootstrap extracted
	asset="mise-v${mise_version}-${platform_id}.${platform_ext}"
	if ! expected="$(mise_asset_sha256 "${asset}")"; then
		cat >&2 <<EOF
Unsupported platform: ${platform_label} ${platform_machine} ($(uname -s)/$(uname -m)).
Pinned mise ${mise_version} assets are checked for Linux (x64, arm64, and armv7, including musl), macOS (x64 and arm64), and Git Bash on Windows (x64 and arm64). WSL is treated as Linux.
Install mise ${mise_version} yourself, put it on PATH, and re-run this script.
EOF
		return 1
	fi

	if ! command -v curl >/dev/null 2>&1; then
		echo "curl is required to download mise ${mise_version}. Install curl and re-run." >&2
		return 1
	fi
	if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
		echo "sha256sum or shasum is required to verify the mise download." >&2
		return 1
	fi
	if [[ "${platform_ext}" == "tar.gz" ]] && ! command -v tar >/dev/null 2>&1; then
		echo "tar is required to unpack the mise archive." >&2
		return 1
	fi

	url="https://github.com/jdx/mise/releases/download/v${mise_version}/${asset}"
	bootstrap="$(mktemp -d)"
	echo "Downloading ${url}"
	if ! curl -fsSL --retry 3 --retry-delay 2 -o "${bootstrap}/${asset}" "${url}"; then
		echo "Could not download ${url}." >&2
		return 1
	fi
	if ! sha256_check "${bootstrap}/${asset}" "${expected}"; then
		echo "mise download checksum did not match the pinned SHA-256 for ${asset}." >&2
		return 1
	fi

	mkdir -p "${HOME}/.local/bin"
	if [[ "${platform_ext}" == "exe" ]]; then
		extracted="${bootstrap}/${asset}"
		mise_dest="${HOME}/.local/bin/mise.exe"
	else
		if ! tar -xzf "${bootstrap}/${asset}" -C "${bootstrap}"; then
			echo "Could not unpack ${asset}." >&2
			return 1
		fi
		extracted="${bootstrap}/mise/bin/mise"
		mise_dest="${HOME}/.local/bin/mise"
		if [[ ! -f "${extracted}" ]]; then
			echo "The mise archive did not contain mise/bin/mise." >&2
			return 1
		fi
	fi

	if command -v install >/dev/null 2>&1; then
		install -m 0755 "${extracted}" "${mise_dest}.new"
	else
		cp "${extracted}" "${mise_dest}.new"
		chmod 0755 "${mise_dest}.new"
	fi
	mv -f "${mise_dest}.new" "${mise_dest}"
	hash -r || true
	if ! mise_version_matches "${mise_dest}"; then
		echo "The installed mise binary did not report ${mise_version}." >&2
		return 1
	fi
	echo "Installed mise ${mise_version} at ${mise_dest}."
}

install_main() {
	local existing
	problems=0
	bootstrap=""
	mise_dest="${HOME}/.local/bin/mise"
	trap 'if [[ -n "${bootstrap}" ]]; then rm -rf "${bootstrap}"; fi' EXIT

	export PATH="${HOME}/.local/bin:${PATH}"
	export MISE_CACHE_DIR="${repo_root}/.direnv/mise-cache"
	mkdir -p "${MISE_CACHE_DIR}"
	"${repo_root}/scripts/repair-volatile-cache-links.sh"
	"${repo_root}/scripts/install-codespace-self-heal.sh"

	detect_platform
	echo "Platform: ${platform_label} ${platform_machine}."

	existing="$(command -v mise 2>/dev/null || true)"
	if [[ -n "${existing}" ]] && mise_version_matches "${existing}"; then
		echo "mise ${mise_version} is already installed (${existing})."
		if [[ -x "${HOME}/.local/bin/mise.exe" && ! -x "${HOME}/.local/bin/mise" ]]; then
			mise_dest="${HOME}/.local/bin/mise.exe"
		elif [[ -x "${existing}" ]]; then
			mise_dest="${existing}"
		fi
	else
		install_mise_asset
	fi

	# Trust before activation so the first read of mise.toml does not warn.
	hash -r || true
	"${mise_dest}" trust "${repo_root}/mise.toml"
	# shellcheck disable=SC1090
	eval "$("${mise_dest}" activate bash)"
	hash -r || true

	# mise.toml sets lockfile, locked provenance verification, and minimum
	# release age. Do not pass flags that turn those off.
	mise install --locked --yes

	if [[ "$(uname -s)" == "Linux" && "$(uname -m)" == "x86_64" ]]; then
		mise run install-cursor
	else
		echo "Skipped the Cursor agent install task. Its pinned asset is for Linux x86_64."
	fi
	mise exec -- direnv allow "${repo_root}"

	echo
	echo "Host prerequisites outside mise:"
	if ! command -v git >/dev/null 2>&1; then
		case "${platform_label}" in
		macOS)
			problem "Git is not on PATH. Install the Xcode command line tools or a Git package, then re-run."
			;;
		"Windows Git Bash")
			problem "Git is not on PATH. Install Git for Windows and re-run this script from Git Bash."
			;;
		*)
			problem "Git is not on PATH. Install Git from your system packages and re-run."
			;;
		esac
	else
		echo "Git: $(git --version)"
	fi
	if ! command -v curl >/dev/null 2>&1; then
		problem "curl is not on PATH. The demo uses it to reach the local Hub and registry."
	else
		echo "curl: present"
	fi
	if ! command -v tar >/dev/null 2>&1; then
		problem "tar is not on PATH. The demo unpacks the Temporal CLI with it."
	else
		echo "tar: present"
	fi
	if ! command -v sha256sum >/dev/null 2>&1; then
		problem "sha256sum is not on PATH. scripts/fetch-temporal-cli.sh checks its download with sha256sum."
	else
		echo "sha256sum: present"
	fi
	if ! command -v python3 >/dev/null 2>&1; then
		problem "python3 is not on PATH. scripts/demo-seed.sh uses it to publish the sample chart and read JSON."
	else
		echo "python3: $(python3 --version 2>&1)"
	fi
	if ! command -v go >/dev/null 2>&1; then
		problem "go is not on PATH after mise install. Activate mise and re-run."
	else
		echo "go: $(go version)"
	fi
	if ! date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ >/dev/null 2>&1; then
		problem "GNU date is required (date -d). scripts/demo-seed.sh uses it. Install GNU coreutils and put that date on PATH."
	else
		echo "GNU date: present"
	fi
	if ! command -v seq >/dev/null 2>&1; then
		problem "seq is not on PATH. scripts/demo-seed.sh uses it while waiting for local services."
	fi
	if ! command -v pkill >/dev/null 2>&1; then
		problem "pkill is not on PATH. scripts/demo-reset.sh uses it to stop the previous demo."
	fi
	if ! command -v ss >/dev/null 2>&1; then
		echo "Note: ss is not on PATH. scripts/demo-reset.sh uses it to release listeners and continues when it is absent. Install iproute2 if a port stays busy."
	fi
	if ! command -v docker >/dev/null 2>&1; then
		problem "Docker is not on PATH. scripts/demo-seed.sh needs it to publish the sample image and chart. Install Docker for ${platform_label} and start its daemon. This installer does not install Docker and does not use administrator privileges."
	elif ! docker info >/dev/null 2>&1; then
		problem "Docker is installed but the daemon is not reachable. Start the daemon and re-run. This installer does not start Docker and does not use administrator privileges."
	else
		echo "Docker daemon: reachable"
		echo "If publishing ports fails because the daemon has no iptables or bridge networking, run: DEMO_DOCKER_NETWORK=host mise run demo"
	fi

	echo
	if [[ "${problems}" -ne 0 ]]; then
		echo "The pinned tools are installed. The demo still needs the ${problems} item(s) above. Re-running this installer is safe." >&2
		return 1
	fi

	echo "Toolchain is ready."
	if [[ "${CONDUIT_INSTALL_SOURCED:-0}" == "1" ]]; then
		echo "mise will be activated in the shell that sourced this installer."
	else
		echo "Activate mise in this shell with:"
		echo "  eval \"\$(${mise_dest} activate bash)\""
		echo "  eval \"\$(${mise_dest} activate zsh)\""
	fi
	echo "Then start the demo with: mise run demo"
	echo "When it finishes, open http://127.0.0.1:8081/console/"
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONDUIT_INSTALL_SOURCED="${sourced}"
status=0
(
	set -euo pipefail
	cd "${repo_root}"
	install_main
) || status=$?

if [[ "${status}" -ne 0 ]]; then
	if [[ "${sourced}" -eq 1 ]]; then
		return "${status}"
	fi
	exit "${status}"
fi

if [[ "${sourced}" -eq 1 ]]; then
	mise_path="${HOME}/.local/bin/mise"
	if [[ ! -x "${mise_path}" && -x "${HOME}/.local/bin/mise.exe" ]]; then
		mise_path="${HOME}/.local/bin/mise.exe"
	fi
	# shellcheck disable=SC1090
	eval "$("${mise_path}" activate bash)"
	hash -r || true
	echo "Activated mise in this shell."
fi
unset CONDUIT_INSTALL_SOURCED status
