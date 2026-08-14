#!/usr/bin/env bash
set -euo pipefail

export PATH="${HOME}/.local/bin:${PATH}"
export MISE_CACHE_DIR="${PWD}/.direnv/mise-cache"
mkdir -p "${MISE_CACHE_DIR}"
./scripts/repair-volatile-cache-links.sh
./scripts/install-codespace-self-heal.sh

mise_version="2026.8.3"
if ! command -v mise >/dev/null 2>&1 || [ "$(mise --version | awk '{print $1}')" != "${mise_version}" ]; then
	case "$(uname -s)/$(uname -m)" in
		Linux/x86_64) archive_sha256="8aaf21cc4b36681e90a96e9cdf13e5d7511e9773733f741b1a5f7756ba53b5fc" ;;
		*) echo "unsupported mise bootstrap platform: $(uname -s)/$(uname -m)" >&2; exit 1 ;;
	esac
	bootstrap="$(mktemp -d)"
	trap 'rm -rf "${bootstrap}"' EXIT
	curl -fsSL "https://github.com/jdx/mise/releases/download/v${mise_version}/mise-v${mise_version}-linux-x64.tar.gz" -o "${bootstrap}/mise.tar.gz"
	echo "${archive_sha256}  ${bootstrap}/mise.tar.gz" | sha256sum --check
	tar -xzf "${bootstrap}/mise.tar.gz" -C "${bootstrap}"
	mkdir -p "${HOME}/.local/bin"
	install -m 0755 "${bootstrap}/mise/bin/mise" "${HOME}/.local/bin/mise.new"
	mv "${HOME}/.local/bin/mise.new" "${HOME}/.local/bin/mise"
fi

mise install
mise run install-cursor
mise exec -- direnv allow "${PWD}"
