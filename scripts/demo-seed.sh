#!/usr/bin/env bash
# Start a local Hub, one packaging worker, and one sample application, then
# record a packaging run, a signed bundle, a change request, its approval,
# a promotion from DEV to STAGING, and a recall. Running this again clears
# the previous copy first.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
state_dir="${CONDUIT_SEED_DIR:-/tmp/conduit-seed}"
"${root}/scripts/demo-reset.sh"

mkdir -p "${state_dir}/data" "${state_dir}/packaging"
pid_file="${state_dir}/pids"
: >"${pid_file}"

if ! "${root}/scripts/fetch-temporal-cli.sh" >/dev/null; then
  echo "Temporal CLI could not be fetched" >&2
  exit 1
fi
cli="${root}/.cache/temporal/temporal"

nohup env PACKAGING_TEMPORAL_DB="${state_dir}/temporal.db" \
  PACKAGING_TEMPORAL_PORT=7233 \
  PACKAGING_TEMPORAL_UI_PORT=8233 \
  "${root}/scripts/packaging-temporal-server.sh" >"${state_dir}/temporal.log" 2>&1 &
echo $! >>"${pid_file}"

for _ in $(seq 1 60); do
  if (echo >/dev/tcp/127.0.0.1/7233) >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! (echo >/dev/tcp/127.0.0.1/7233) >/dev/null 2>&1; then
  echo "Temporal did not accept connections on 127.0.0.1:7233" >&2
  tail -20 "${state_dir}/temporal.log" >&2 || true
  exit 1
fi

nohup go run ./cmd/packaging-workflow worker \
  --address 127.0.0.1:7233 \
  --state-dir "${state_dir}/packaging" >"${state_dir}/worker.log" 2>&1 &
echo $! >>"${pid_file}"
sleep 2

nohup env LISTEN_ADDRESS=127.0.0.1:8081 \
  DATA_DIRECTORY="${state_dir}/data" \
  ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_AGENT_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY=true \
  PACKAGING_TEMPORAL_ADDRESS=127.0.0.1:7233 \
  go run ./cmd/platform-delivery >"${state_dir}/hub.log" 2>&1 &
echo $! >>"${pid_file}"

hub="http://127.0.0.1:8081"
for _ in $(seq 1 90); do
  if curl -fsS "${hub}/version" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl -fsS "${hub}/version" >/dev/null 2>&1; then
  echo "Hub did not answer ${hub}/version" >&2
  tail -30 "${state_dir}/hub.log" >&2 || true
  exit 1
fi

go run ./cmd/packaging-workflow run sample-app-1 --address 127.0.0.1:7233

api() {
  local method="$1" path="$2" body="${3:-}"
  if [[ -n "${body}" ]]; then
    curl -fsS -X "${method}" "${hub}${path}" \
      -H 'content-type: application/json' \
      -H 'accept: application/json' \
      --data "${body}"
  else
    curl -fsS -X "${method}" "${hub}${path}" -H 'accept: application/json'
  fi
}

api PUT /v1/teams/release-reviewers "$(cat <<'JSON'
{"name":"release-reviewers","displayName":"Release reviewers","adminGroups":["reviewers"],"memberGroups":["reviewers"]}
JSON
)" >/dev/null

api PUT /v1/approval-policies/release-review "$(cat <<'JSON'
{"id":"release-review","displayName":"Release review","enabled":true,"expression":"type != \"\" ? require_team(\"release-reviewers\", \"release review\") : no_op()"}
JSON
)" >/dev/null

plan_file="${state_dir}/plan.json"
api POST /v1/plans "$(cat <<'JSON'
{
  "apiVersion": "platform.delivery/v1alpha1",
  "kind": "EnvironmentDeployment",
  "environment": {"id": "dev", "generation": "0123456789abcdef"},
  "releaseChannels": [
    {"name": "DEV", "description": "Development"},
    {"name": "STAGING", "description": "Staging"}
  ],
  "releases": [
    {
      "product": "app.example:sample-app",
      "version": "0.1.0",
      "channels": ["DEV"],
      "chart": {"repository": "registry.example/sample-app", "tag": "0.1.0", "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
      "images": [{"repository": "registry.example/sample-app", "tag": "0.1.0", "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]
    },
    {
      "product": "app.example:sample-app",
      "version": "0.2.0",
      "channels": ["DEV"],
      "chart": {"repository": "registry.example/sample-app", "tag": "0.2.0", "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
      "images": [{"repository": "registry.example/sample-app", "tag": "0.2.0", "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]
    }
  ],
  "recalls": [
    {
      "id": "rcl-sample-010",
      "product": "app.example:sample-app",
      "startingVersion": "0.1.0",
      "singleVersion": true,
      "reason": "sample release failed its release check",
      "strategy": {"type": "ROLL_FORWARD"}
    }
  ],
  "productSettings": [
    {
      "product": "app.example:sample-app",
      "promotionPipeline": {
        "enabled": true,
        "stages": [{"fromChannel": "DEV", "toChannel": "STAGING", "policy": {"noDelay": true, "minimumHealthyEntities": 0}}]
      }
    }
  ],
  "entities": [
    {"name": "sample-app", "product": "app.example:sample-app", "version": "0.2.0", "namespace": "sample"}
  ]
}
JSON
)" >"${plan_file}"

change_id="$(python3 - <<PY
import json
print(json.load(open("${plan_file}"))["changeRequestId"])
PY
)"
curl -fsS -X POST "${hub}/v1/change-requests/${change_id}/approve" \
  -H 'content-type: application/json' \
  -H 'X-Platform-Subject: reviewer' \
  -H 'X-Platform-Groups: reviewers' \
  --data '{"rationale":"sample release accepted"}' >/dev/null

lease_file="${state_dir}/lease.json"
curl -fsS -X POST "${hub}/v1/environments/dev/leases" \
  -H 'content-type: application/json' \
  -H 'X-Platform-Environment-ID: dev' \
  -H 'X-Platform-Agent-ID: spoke-dev' \
  --data '{"durationSeconds":120}' >"${lease_file}"
plan_id="$(python3 -c 'import json; print(json.load(open("'"${lease_file}"'"))["id"])')"
token="$(python3 -c 'import json; print(json.load(open("'"${lease_file}"'"))["lease"]["token"])')"

curl -fsS -X POST "${hub}/v1/plans/${plan_id}/start" \
  -H 'X-Platform-Environment-ID: dev' \
  -H 'X-Platform-Agent-ID: spoke-dev' \
  -H "X-Platform-Lease-Token: ${token}" >/dev/null

python3 - <<PY >"${state_dir}/finish.json"
import json
lease = json.load(open("${lease_file}"))
steps = lease["plan"]["steps"]
entities = []
tasks = []
for index, step in enumerate(steps, start=1):
    entities.append({
        "installationId": step["installationId"],
        "product": step["product"],
        "version": step["version"],
        "releaseDigest": step["releaseDigest"],
        "namespace": step["namespace"],
        "configDigest": step["configDigest"],
        "health": "HEALTHY",
        "rolloutStatus": "UP_TO_DATE",
        "configStatus": "APPLIED",
        "lifecycle": "INSTALLED",
    })
    tasks.append({
        "sequence": index,
        "phase": "APPLY",
        "status": "SUCCEEDED",
        "installationId": step["installationId"],
        "namespace": step["namespace"],
        "startedAt": "2026-11-14T06:00:00Z",
        "finishedAt": "2026-11-14T06:00:01Z",
    })
json.dump({
    "status": "SUCCEEDED",
    "execution": {
        "startedAt": "2026-11-14T06:00:00Z",
        "finishedAt": "2026-11-14T06:00:02Z",
        "tasks": tasks,
    },
    "entities": entities,
}, __import__("sys").stdout)
PY

curl -fsS -X POST "${hub}/v1/plans/${plan_id}/finish" \
  -H 'content-type: application/json' \
  -H 'X-Platform-Environment-ID: dev' \
  -H 'X-Platform-Agent-ID: spoke-dev' \
  -H "X-Platform-Lease-Token: ${token}" \
  --data @"${state_dir}/finish.json" >/dev/null

now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
hour_ago="$(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)"
api POST /v1/promotions/evaluations "$(cat <<JSON
{
  "release": {
    "product": "app.example:sample-app",
    "version": "0.2.0",
    "channels": ["DEV"],
    "chart": {"repository": "registry.example/sample-app", "tag": "0.2.0", "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
    "images": [{"repository": "registry.example/sample-app", "tag": "0.2.0", "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]
  },
  "fromChannel": "DEV",
  "toChannel": "STAGING",
  "policy": {"noDelay": true, "minimumHealthyEntities": 1, "maximumUnhealthyEntities": 0},
  "evidence": [{
    "environment": "dev",
    "installationId": "sample-app",
    "health": "HEALTHY",
    "healthySince": "${hour_ago}",
    "observedAt": "${now}"
  }]
}
JSON
)" >"${state_dir}/promotion.json"

api PUT /v1/export-pipelines/sample-app "$(cat <<'JSON'
{
  "id": "sample-app",
  "displayName": "Sample application",
  "buildStrategy": "SNAPSHOT",
  "selections": [{
    "type": "ENVIRONMENT",
    "ids": ["dev"],
    "releaseStrategy": "ALL",
    "releaseChannels": ["DEV"]
  }]
}
JSON
)" >/dev/null

api POST /v1/export-pipelines/sample-app/build >"${state_dir}/build.json"
bundle_id="$(python3 -c 'import json; print(json.load(open("'"${state_dir}"'/build.json"))["bundles"][0]["manifest"]["id"])')"
api GET "/v1/bundles/${bundle_id}" >"${state_dir}/bundle.json"
curl -fsS -X POST "${hub}/v1/bundles/preview" \
  -H 'content-type: application/json' \
  --data @"${state_dir}/bundle.json" >"${state_dir}/preview.json"
python3 - <<PY
import json
preview = json.load(open("${state_dir}/preview.json"))
assert preview.get("manifestDigest"), preview
assert preview.get("signature", {}).get("verified") is True, preview
assert preview.get("sbom"), preview
print(preview["bundleId"], preview["manifestDigest"])
PY

for _ in $(seq 1 60); do
  if curl -fsS "${hub}/v1/packaging-runs/sample-app-1" | python3 -c 'import json,sys; body=json.load(sys.stdin); raise SystemExit(0 if body.get("phase")=="review" else 1)'; then
    break
  fi
  sleep 1
done
phase="$(curl -fsS "${hub}/v1/packaging-runs/sample-app-1" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("phase",""))')"
if [[ "${phase}" != "review" ]]; then
  echo "packaging run sample-app-1 phase is ${phase}, want review" >&2
  exit 1
fi

echo "Hub ${hub}"
echo "console ${hub}/console/"
echo "packaging run sample-app-1 is in review"
echo "bundle ${bundle_id}"
echo "change request ${change_id} approved"
