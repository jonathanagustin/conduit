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
ln -s "${volatile}/helm" "${links}/helm"
ln -s "${volatile}/playwright" "${links}/playwright"
ln -s "${volatile}/cursor-versions" "${links}/cursor-versions"
ln -s "${mise_volatile}/mise" "${links}/mise"

CONDUIT_VOLATILE_CACHE_ROOT="${volatile}" \
CONDUIT_GO_MOD_LINK="${links}/go-mod" \
CONDUIT_GO_BUILD_LINK="${links}/go-build" \
CONDUIT_HELM_CACHE_LINK="${links}/helm" \
CONDUIT_PLAYWRIGHT_LINK="${links}/playwright" \
CONDUIT_CURSOR_VERSIONS_LINK="${links}/cursor-versions" \
CONDUIT_MISE_VOLATILE_CACHE_ROOT="${mise_volatile}" \
CONDUIT_MISE_DATA_LINK="${links}/mise" \
./scripts/repair-volatile-cache-links.sh

for target in go-mod go-build helm playwright cursor-versions; do
	[[ -d "${volatile}/${target}" ]]
done
[[ -d "${mise_volatile}/mise" ]]

# Idempotence is part of restart safety.
CONDUIT_VOLATILE_CACHE_ROOT="${volatile}" \
CONDUIT_GO_MOD_LINK="${links}/go-mod" \
CONDUIT_GO_BUILD_LINK="${links}/go-build" \
CONDUIT_HELM_CACHE_LINK="${links}/helm" \
CONDUIT_PLAYWRIGHT_LINK="${links}/playwright" \
CONDUIT_CURSOR_VERSIONS_LINK="${links}/cursor-versions" \
CONDUIT_MISE_VOLATILE_CACHE_ROOT="${mise_volatile}" \
CONDUIT_MISE_DATA_LINK="${links}/mise" \
./scripts/repair-volatile-cache-links.sh

ln -s "${fixture}/outside" "${links}/outside"
if CONDUIT_VOLATILE_CACHE_ROOT="${volatile}" \
	CONDUIT_GO_MOD_LINK="${links}/outside" \
	CONDUIT_GO_BUILD_LINK="${fixture}/absent" \
	CONDUIT_HELM_CACHE_LINK="${fixture}/absent-helm" \
	CONDUIT_PLAYWRIGHT_LINK="${fixture}/absent-too" \
	CONDUIT_CURSOR_VERSIONS_LINK="${fixture}/also-absent" \
	CONDUIT_MISE_DATA_LINK="${fixture}/mise-absent" \
	./scripts/repair-volatile-cache-links.sh 2>/dev/null; then
	echo "repair accepted an offload target outside the validated root" >&2
	exit 1
fi
