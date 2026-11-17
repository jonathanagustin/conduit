#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
stage="$(mktemp -d)"
trap 'rm -rf "${stage}"' EXIT
mkdir -p "${stage}/bin"
cat >"${stage}/bin/docker" <<'SH'
#!/usr/bin/env bash
: >"${MODEL_GUARD_MARKER}"
exit 1
SH
chmod +x "${stage}/bin/docker"
export PATH="${stage}/bin:${PATH}" MODEL_GUARD_MARKER="${stage}/docker-called"
unset CONDUIT_MODEL_CMD CONDUIT_ASSISTANT_MODEL_CMD CONDUIT_PLANNING_MODEL_CMD CONDUIT_PACKAGING_MODEL_CMD CONDUIT_APPGEN_MODEL_CMD
export PACKAGING_REPO_URL=https://example.invalid/source CONDUIT_MODEL_BASE_URL= CONDUIT_MODEL_NAME=
expect_refusal() {
  local expected="$1" status=0
  bash "${root}/scripts/governed-packaging-run.sh" >"${stage}/output" 2>&1 || status=$?
  [[ "${status}" == 64 ]] || { cat "${stage}/output"; exit 1; }
  grep -qF "${expected}" "${stage}/output"
  [[ ! -e "${MODEL_GUARD_MARKER}" ]]
  ! grep -qF 'private-command-value' "${stage}/output"
}
expect_refusal 'requires explicit CONDUIT_MODEL_BASE_URL and CONDUIT_MODEL_NAME'
export CONDUIT_MODEL_BASE_URL=https://models.example.invalid/v1 CONDUIT_MODEL_NAME=selected
for key in CONDUIT_MODEL_CMD CONDUIT_ASSISTANT_MODEL_CMD CONDUIT_PLANNING_MODEL_CMD CONDUIT_PACKAGING_MODEL_CMD CONDUIT_APPGEN_MODEL_CMD; do
  export "${key}=private-command-value"
  expect_refusal "${key} is unsupported"
  unset "${key}"
done
status=0
bash "${root}/scripts/governed-packaging-run.sh" >"${stage}/output" 2>&1 || status=$?
[[ "${status}" == 69 && -e "${MODEL_GUARD_MARKER}" ]]
grep -qF 'Docker is required' "${stage}/output"
echo 'PASS governed packaging: explicit HTTP admitted, legacy commands and implicit live fixture refused before tools'
