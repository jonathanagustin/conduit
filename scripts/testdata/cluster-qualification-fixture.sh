#!/usr/bin/env bash
# Offline GitHub API fixture shared by qualification and release-entry tests.
cluster_qualification_fixture() {
  local directory="$1" sha="$2" workflow id run_id job_id
  mkdir -p "${directory}"
  for workflow in platform-e2e.yml platform-gitless-e2e.yml; do
    id=10; run_id=100; job_id=1000
    [[ "${workflow}" != platform-gitless-e2e.yml ]] || { id=20; run_id=200; job_id=2000; }
    jq -n --arg file "${workflow}" --argjson id "${id}" \
      '{id:$id,path:(".github/workflows/"+$file),state:"active"}' >"${directory}/${workflow}-metadata.json"
    jq -n --arg file "${workflow}" --arg sha "${sha}" --argjson id "${id}" --argjson run "${run_id}" \
      '{id:$run,run_number:7,run_attempt:2,workflow_id:$id,path:(".github/workflows/"+$file),event:"workflow_dispatch",head_sha:$sha,repository:{full_name:"jonathanagustin/conduit"},head_repository:{full_name:"jonathanagustin/conduit"},status:"completed",conclusion:"success"}' \
      >"${directory}/run-${run_id}.json"
    jq '{total_count:1,workflow_runs:[.]}' "${directory}/run-${run_id}.json" >"${directory}/${workflow}-runs.json"
    jq --arg file "${workflow}" --arg sha "${sha}" --argjson run "${run_id}" --argjson id "${job_id}" '
      .workflows[] | select(.file == $file) | . as $spec |
      {total_count:(.jobs|length),jobs:[.jobs | to_entries[] |
        {id:($id+.key),name:.value,run_id:$run,head_sha:$sha,
         run_url:("https://api.github.com/repos/jonathanagustin/conduit/actions/runs/"+($run|tostring)),
         status:"completed",conclusion:"success",steps:[$spec.steps[] | {name:.,status:"completed",conclusion:"success"}]}]}
    ' "${repository_root}/scripts/cluster-qualification.json" >"${directory}/jobs-${run_id}-2.json"
  done
}

cluster_qualification_fake_gh() {
  cat <<'SCRIPT'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == api ]] || exit 1
path="$2"
printf '%s\n' "${path}" >>"${RELEASE_FIXTURE}/requests"
file=""
case "${path}" in
  */actions/workflows/*/runs\?*) workflow="${path#*/actions/workflows/}"; file="${workflow%%/*}-runs.json" ;;
  */actions/workflows/*) file="${path##*/}-metadata.json" ;;
  */actions/runs/*/attempts/*/jobs\?*) rest="${path#*/actions/runs/}"; run="${rest%%/*}"; rest="${rest#*/attempts/}"; file="jobs-${run}-${rest%%/*}.json" ;;
  */actions/runs/*) file="run-${path##*/}.json" ;;
  */git/matching-refs/tags/*) file=tags.json ;;
  */compare/*) file=compare.json ;;
  */commits/*/status) sha="${path#*/commits/}"; file="status-${sha%/status}.json" ;;
  */commits/*/pulls) sha="${path#*/commits/}"; file="pulls-${sha%/pulls}.json" ;;
esac
if [[ -e "${RELEASE_FIXTURE}/api-error" || -z "${file}" || ! -f "${RELEASE_FIXTURE}/${file}" ]]; then
  echo "gh: Not Found (HTTP 404) ${path}" >&2
  exit 1
fi
if [[ -e "${RELEASE_FIXTURE}/malformed-json" ]]; then printf '{'; exit; fi
if [[ "${file}" == run-100.json && -e "${RELEASE_FIXTURE}/change-attempt" ]]; then
  calls="$(grep -c '/actions/runs/100$' "${RELEASE_FIXTURE}/requests" || true)"
  if (( calls > 1 )); then jq '.run_attempt += 1' "${RELEASE_FIXTURE}/${file}"; exit; fi
fi
if [[ "${file}" == platform-e2e.yml-runs.json && -e "${RELEASE_FIXTURE}/new-dispatch" ]]; then
  calls="$(grep -c '/actions/workflows/platform-e2e.yml/runs?' "${RELEASE_FIXTURE}/requests" || true)"
  if (( calls > 1 )); then jq '.workflow_runs[0].id += 1 | .workflow_runs[0].run_number += 1' "${RELEASE_FIXTURE}/${file}"; exit; fi
fi
cat "${RELEASE_FIXTURE}/${file}"
SCRIPT
}
