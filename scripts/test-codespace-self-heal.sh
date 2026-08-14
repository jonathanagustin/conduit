#!/usr/bin/env bash
set -euo pipefail

fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
volatile="${fixture}/volatile"
links="${fixture}/links"
state="${fixture}/state"
home="${fixture}/home"
mkdir -p "${volatile}" "${links}" "${home}"

ln -s "${volatile}/go-mod" "${links}/go-mod"
ln -s "${volatile}/go-build" "${links}/go-build"
ln -s "${volatile}/helm" "${links}/helm"
ln -s "${volatile}/playwright" "${links}/playwright"
ln -s "${volatile}/cursor" "${links}/cursor"
ln -s "${volatile}/mise" "${links}/mise"

run_heal() {
	CONDUIT_REPO_ROOT="${PWD}" \
	CONDUIT_BOOT_STATE_ROOT="${state}" \
	CONDUIT_VOLATILE_CACHE_ROOT="${volatile}" \
	CONDUIT_GO_MOD_LINK="${links}/go-mod" \
	CONDUIT_GO_BUILD_LINK="${links}/go-build" \
	CONDUIT_HELM_CACHE_LINK="${links}/helm" \
	CONDUIT_PLAYWRIGHT_LINK="${links}/playwright" \
	CONDUIT_CURSOR_VERSIONS_LINK="${links}/cursor" \
	CONDUIT_MISE_VOLATILE_CACHE_ROOT="${volatile}" \
	CONDUIT_MISE_DATA_LINK="${links}/mise" \
	./scripts/codespace-self-heal.sh
}

run_heal
for target in go-mod go-build helm playwright cursor mise; do
	[[ -d "${volatile}/${target}" ]]
done
[[ -f "${state}/complete" ]]

# A second shell in the same volatile-disk lifetime is a no-op.
before="$(cat "${state}/complete")"
run_heal
[[ "$(cat "${state}/complete")" == "${before}" ]]

# A /tmp wipe removes both the targets and marker; the next shell repairs all.
rm -rf "${volatile}" "${state}"
mkdir -p "${volatile}"
run_heal
[[ -d "${volatile}/go-mod" && -d "${volatile}/helm" && -d "${volatile}/mise" ]]

bashrc="${home}/.bashrc"
for _ in 1 2; do
	CODESPACES=true CONDUIT_REPO_ROOT="${PWD}" CONDUIT_BASHRC="${bashrc}" \
		./scripts/install-codespace-self-heal.sh
done
[[ "$(grep -c '^# >>> conduit codespace self-heal >>>$' "${bashrc}")" -eq 1 ]]
[[ "$(grep -c '^# <<< conduit codespace self-heal <<<$' "${bashrc}")" -eq 1 ]]
grep -Fq "${PWD}/scripts/codespace-self-heal.sh" "${bashrc}"

echo "PASS Codespaces self-heal: restart repair=6 targets, idempotent hook=1"
