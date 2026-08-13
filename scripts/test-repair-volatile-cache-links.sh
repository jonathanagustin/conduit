#!/usr/bin/env bash
set -euo pipefail

fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
volatile="${fixture}/volatile"
mise_volatile="${fixture}/mise-volatile"
links="${fixture}/links"
mkdir -p "${volatile}" "${mise_volatile}" "${links}"
ln -s "${volatile}/go-mod" "${links}/go-mod"
ln -s "${volatile}/go-build" "${links}/go-build"
ln -s "${volatile}/playwright" "${links}/playwright"
ln -s "${volatile}/cursor-versions" "${links}/cursor-versions"
ln -s "${mise_volatile}/mise" "${links}/mise"

PLATFORM_VOLATILE_CACHE_ROOT="${volatile}" \
PLATFORM_GO_MOD_LINK="${links}/go-mod" \
PLATFORM_GO_BUILD_LINK="${links}/go-build" \
PLATFORM_PLAYWRIGHT_LINK="${links}/playwright" \
PLATFORM_CURSOR_VERSIONS_LINK="${links}/cursor-versions" \
PLATFORM_MISE_VOLATILE_CACHE_ROOT="${mise_volatile}" \
PLATFORM_MISE_DATA_LINK="${links}/mise" \
./scripts/repair-volatile-cache-links.sh

for target in go-mod go-build playwright cursor-versions; do
	[[ -d "${volatile}/${target}" ]]
done
[[ -d "${mise_volatile}/mise" ]]

# Idempotence is part of restart safety.
PLATFORM_VOLATILE_CACHE_ROOT="${volatile}" \
PLATFORM_GO_MOD_LINK="${links}/go-mod" \
PLATFORM_GO_BUILD_LINK="${links}/go-build" \
PLATFORM_PLAYWRIGHT_LINK="${links}/playwright" \
PLATFORM_CURSOR_VERSIONS_LINK="${links}/cursor-versions" \
PLATFORM_MISE_VOLATILE_CACHE_ROOT="${mise_volatile}" \
PLATFORM_MISE_DATA_LINK="${links}/mise" \
./scripts/repair-volatile-cache-links.sh

ln -s "${fixture}/outside" "${links}/outside"
if PLATFORM_VOLATILE_CACHE_ROOT="${volatile}" \
	PLATFORM_GO_MOD_LINK="${links}/outside" \
	PLATFORM_GO_BUILD_LINK="${fixture}/absent" \
	PLATFORM_PLAYWRIGHT_LINK="${fixture}/absent-too" \
	PLATFORM_CURSOR_VERSIONS_LINK="${fixture}/also-absent" \
	PLATFORM_MISE_DATA_LINK="${fixture}/mise-absent" \
	./scripts/repair-volatile-cache-links.sh 2>/dev/null; then
	echo "repair accepted an offload target outside the validated root" >&2
	exit 1
fi
