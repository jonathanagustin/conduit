#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
test_root="$(mktemp -d)"
hub_pid=""
hub_url=""

cleanup() {
  if [[ -n "${hub_pid}" ]] && kill -0 "${hub_pid}" 2>/dev/null; then
    kill "${hub_pid}" 2>/dev/null || true
    wait "${hub_pid}" 2>/dev/null || true
  fi
  rm -rf "${test_root}"
}
trap cleanup EXIT

go build -o "${test_root}/platform-delivery" ./cmd/platform-delivery
go build -o "${test_root}/platform-module-publisher" ./cmd/platform-module-publisher
cp -R examples/modules "${test_root}/modules"

start_hub() {
  # Fixed ports make the acceptance test interfere with legitimate operator
  # tunnels and concurrent worktrees. Ask the kernel for an unused loopback
  # port immediately before launch; the Hub never binds beyond loopback.
  local hub_port
  hub_port="$(node -e 'const net=require("node:net");const s=net.createServer();s.listen(0,"127.0.0.1",()=>{process.stdout.write(String(s.address().port));s.close();});')"
  hub_url="http://127.0.0.1:${hub_port}"
  # This acceptance Hub mounts no signing Secret, so it has to say so. A Hub
  # that has not declared itself a development Hub now refuses to start rather
  # than signing Bundles with a key it generated for itself.
  ALLOW_INSECURE_AGENT_IDENTITY_HEADERS=true \
    ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS=true \
    ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY=true \
    LISTEN_ADDRESS="127.0.0.1:${hub_port}" \
    DATA_DIRECTORY="${test_root}/state" \
    "${test_root}/platform-delivery" >"${test_root}/hub.log" 2>&1 &
  hub_pid=$!
  for _ in $(seq 1 30); do
    if curl --fail --silent "${hub_url}/health/ready" >/dev/null 2>&1; then
      return
    fi
    sleep 0.2
  done
  sed -n '1,200p' "${test_root}/hub.log" >&2
  echo "Hub did not become ready" >&2
  exit 1
}

start_hub

"${test_root}/platform-module-publisher" \
  --directory "${test_root}/modules" --hub-url "${hub_url}" \
  >"${test_root}/publication-v1.json"
jq --exit-status '.releases | length == 3' "${test_root}/publication-v1.json" >/dev/null
jq --exit-status '.releases[] | .version == "0.1.0"' "${test_root}/publication-v1.json" >/dev/null

curl --fail --silent --show-error \
  --header 'content-type: application/json' \
  --data '{"channels":["STABLE"]}' \
  "${hub_url}/v1/modules/example-foundation/releases/0.1.0/promote" \
  >"${test_root}/promotion-v1.json"

curl --fail --silent --show-error \
  --header 'content-type: application/json' \
  --data '{"module":"example-foundation","version":"latest","releaseChannel":"STABLE","variables":{"databaseCidr":"10.20.0.0/24"}}' \
  "${hub_url}/v1/environments/gitops-test/module-installations" \
  >"${test_root}/installation.json"
installation_id="$(jq --raw-output .id "${test_root}/installation.json")"
jq --exit-status '.version == "0.1.0" and .releaseChannel == "STABLE" and (.entities | length == 2)' \
  "${test_root}/installation.json" >/dev/null

# A reviewed Git change creates new immutable child and composite Module
# releases, but it cannot affect the tracked installation before promotion.
sed -i 's/host: example-api/host: example-api-v2/' "${test_root}/modules/example-workspace.yaml"
"${test_root}/platform-module-publisher" \
  --directory "${test_root}/modules" --hub-url "${hub_url}" \
  >"${test_root}/publication-v2.json"
jq --exit-status '[.releases[] | select(.name == "example-foundation" and .version == "0.2.0")] | length == 1' \
  "${test_root}/publication-v2.json" >/dev/null

curl --fail --silent --show-error \
  "${hub_url}/v1/module-installations/${installation_id}/update" \
  >"${test_root}/proposal-before-promotion.json"
jq --exit-status '.targetVersion == "0.1.0" and (.changed | length == 0)' \
  "${test_root}/proposal-before-promotion.json" >/dev/null

curl --fail --silent --show-error \
  --header 'content-type: application/json' \
  --data '{"channels":["STABLE"]}' \
  "${hub_url}/v1/modules/example-foundation/releases/0.2.0/promote" \
  >"${test_root}/promotion-v2.json"
curl --fail --silent --show-error \
  "${hub_url}/v1/module-installations/${installation_id}/update" \
  >"${test_root}/proposal-after-promotion.json"
jq --exit-status '.status == "VALID" and .targetVersion == "0.2.0" and (.changed | length == 1)' \
  "${test_root}/proposal-after-promotion.json" >/dev/null

curl --fail --silent --show-error --request POST \
  "${hub_url}/v1/module-installations/${installation_id}/pause" \
  >"${test_root}/paused.json"
curl --fail --silent --show-error \
  "${hub_url}/v1/module-installations/${installation_id}/update" \
  >"${test_root}/paused-proposal.json"
jq --exit-status '.status == "PAUSED"' "${test_root}/paused-proposal.json" >/dev/null
curl --fail --silent --show-error --request POST \
  "${hub_url}/v1/module-installations/${installation_id}/resume" \
  >"${test_root}/resumed.json"
curl --fail --silent --show-error --request POST \
  "${hub_url}/v1/module-installations/${installation_id}/update" \
  >"${test_root}/updated.json"
jq --exit-status '.version == "0.2.0" and (.entities | length == 2)' "${test_root}/updated.json" >/dev/null

# Crash/restart durability is part of the GitOps contract: the Git source can
# be replayed, while live ownership and applied Module version survive restart.
kill "${hub_pid}"
wait "${hub_pid}"
hub_pid=""
start_hub
curl --fail --silent --show-error \
  "${hub_url}/v1/environments/gitops-test/module-installations" \
  >"${test_root}/restored.json"
jq --exit-status --arg id "${installation_id}" \
  '.installations | length == 1 and .[0].id == $id and .[0].version == "0.2.0"' \
  "${test_root}/restored.json" >/dev/null

echo "GitOps acceptance passed: publish, lock, channel gate, pause/resume, update, and durable restart"
