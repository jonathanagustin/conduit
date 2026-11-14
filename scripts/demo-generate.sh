#!/usr/bin/env bash
# Run one recorded app generation through the existing governed flow.
# Leaves CONDUIT_MODEL_CMD unset, so the Hub serves the checked-in spec.
# The default demo seed does not call this. Set DEMO_GENERATE=1, or run it
# after scripts/demo-seed.sh.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
state_dir="${CONDUIT_SEED_DIR:-/tmp/conduit-seed}"
hub="http://127.0.0.1:8081"
fixture="${state_dir}/packaging/fixture"

if ! curl -fsS http://127.0.0.1:5000/v2/ >/dev/null 2>&1; then
  echo "The local registry is not answering on 127.0.0.1:5000. Run scripts/demo-seed.sh first." >&2
  exit 1
fi
if ! (echo >/dev/tcp/127.0.0.1/7233) >/dev/null 2>&1; then
  echo "Temporal is not accepting connections on 127.0.0.1:7233. Run scripts/demo-seed.sh first." >&2
  exit 1
fi

for port in 8081; do
  while read -r pid; do
    [[ "${pid}" =~ ^[0-9]+$ ]] || continue
    kill "${pid}" >/dev/null 2>&1 || true
  done < <(ss -ltnp "sport = :${port}" 2>/dev/null | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | sort -u)
done
sleep 1

nohup env -u CONDUIT_MODEL_CMD \
  LISTEN_ADDRESS=127.0.0.1:8081 \
  DATA_DIRECTORY="${state_dir}/data" \
  ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_AGENT_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY=true \
  PACKAGING_TEMPORAL_ADDRESS=127.0.0.1:7233 \
  PACKAGING_FIXTURE_DIR="${fixture}" \
  CONDUIT_LOCAL_REGISTRY=127.0.0.1:5000 \
  go run ./cmd/platform-delivery >"${state_dir}/hub-generate.log" 2>&1 &
echo $! >>"${state_dir}/pids"

for _ in $(seq 1 90); do
  if curl -fsS "${hub}/version" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl -fsS "${hub}/version" >/dev/null 2>&1; then
  echo "Hub did not answer ${hub}/version" >&2
  tail -40 "${state_dir}/hub-generate.log" >&2 || true
  exit 1
fi

curl -fsS --max-time 180 -X POST "${hub}/v1/app-generations" \
  -H 'content-type: application/json' \
  -H 'accept: application/json' \
  --data '{"name":"hello-http","description":"a small page that says hello","confirm":true}' \
  >"${state_dir}/generation.json"

python3 - "${state_dir}/generation.json" <<'PY'
import json, sys
body = json.load(open(sys.argv[1]))
if body.get("label") != "No model call":
    raise SystemExit(f"label is {body.get('label')!r}, want No model call")
if body.get("error"):
    raise SystemExit(f"generation failed: {body['error']}\n{json.dumps(body.get('gates'), indent=2)}")
for gate in body.get("gates") or []:
    if gate.get("outcome") != "pass" or not gate.get("reason"):
        raise SystemExit(f"gate {gate.get('name')} is {gate.get('outcome')}: {gate.get('reason')}")
provenance = body.get("provenance") or {}
for field in ("adapter", "promptSha256", "responseSha256"):
    if not provenance.get(field):
        raise SystemExit(f"provenance missing {field}")
files = {item.get("path"): item.get("sha256") for item in body.get("files") or []}
for path in ("main.go", "Dockerfile", "values.yaml", "zarf.yaml", "uds-package.yaml"):
    if not files.get(path):
        raise SystemExit(f"missing digest for {path}")
if not str(body.get("imageDigest", "")).startswith("sha256:"):
    raise SystemExit(f"image digest {body.get('imageDigest')!r}")
if not str(body.get("chartDigest", "")).startswith("sha256:"):
    raise SystemExit(f"chart digest {body.get('chartDigest')!r}")
print(body["runId"], body["imageDigest"], body["chartDigest"])
PY

run_id="$(python3 -c 'import json; print(json.load(open("'"${state_dir}"'/generation.json"))["runId"])')"
image_digest="$(python3 -c 'import json; print(json.load(open("'"${state_dir}"'/generation.json"))["imageDigest"])')"
chart_digest="$(python3 -c 'import json; print(json.load(open("'"${state_dir}"'/generation.json"))["chartDigest"])')"
source_file="${fixture}/sources/${run_id}.txt"
if [[ ! -f "${source_file}" ]]; then
  echo "Packaging source was not written to ${source_file}" >&2
  exit 1
fi

phase="$(curl -fsS "${hub}/v1/packaging-runs/${run_id}" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("phase",""))' || true)"
if [[ "${phase}" != "review" && "${phase}" != "complete" ]]; then
  go run ./cmd/packaging-workflow run "${run_id}" --address 127.0.0.1:7233 --source-file "${source_file}"
fi
phase=""
for _ in $(seq 1 90); do
  phase="$(curl -fsS "${hub}/v1/packaging-runs/${run_id}" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("phase",""))' || true)"
  if [[ "${phase}" == "review" || "${phase}" == "blocked" || "${phase}" == "complete" ]]; then
    break
  fi
  sleep 1
done
if [[ "${phase}" != "review" ]]; then
  echo "packaging run ${run_id} phase is ${phase}, want review" >&2
  exit 1
fi

python3 - "${state_dir}/generation-plan.json" "${image_digest}" "${chart_digest}" "${state_dir}/digests.env" <<'PY'
import hashlib, json, sys, time
image, chart = sys.argv[2], sys.argv[3]
digests = {}
try:
    for line in open(sys.argv[4]):
        if "=" in line:
            key, value = line.strip().split("=", 1)
            digests[key] = value
except FileNotFoundError:
    pass
generation = hashlib.sha256(f"hello-http-{time.time_ns()}".encode()).hexdigest()[:16]
releases = []
entities = []
settings = []
if digests.get("image_020") and digests.get("chart_020"):
    releases.append({
        "product": "app.example:sample-app",
        "version": "0.2.0",
        "channels": ["DEV"],
        "chart": {"repository": "127.0.0.1:5000/sample-app-chart", "tag": "0.2.0", "digest": digests["chart_020"]},
        "images": [{"repository": "127.0.0.1:5000/sample-app", "tag": "0.2.0", "digest": digests["image_020"]}],
    })
    entities.append({
        "name": "sample-app",
        "product": "app.example:sample-app",
        "version": "0.2.0",
        "namespace": "sample",
        "helmValues": {"resources": {"limits": {"memory": "512Mi"}}},
    })
    settings.append({
        "product": "app.example:sample-app",
        "promotionPipeline": {
            "enabled": True,
            "stages": [{"fromChannel": "DEV", "toChannel": "STAGING", "policy": {"noDelay": True, "minimumHealthyEntities": 0}}],
        },
    })
releases.append({
    "product": "app.example:hello-http",
    "version": "0.1.0",
    "channels": ["DEV"],
    "chart": {"repository": "127.0.0.1:5000/hello-http-chart", "tag": "0.1.0", "digest": chart},
    "images": [{"repository": "127.0.0.1:5000/hello-http", "tag": "0.1.0", "digest": image}],
})
entities.append({
    "name": "hello-http",
    "product": "app.example:hello-http",
    "version": "0.1.0",
    "namespace": "hello",
    "helmValues": {"resources": {"limits": {"memory": "64Mi"}}},
})
settings.append({
    "product": "app.example:hello-http",
    "promotionPipeline": {
        "enabled": True,
        "stages": [{"fromChannel": "DEV", "toChannel": "STAGING", "policy": {"noDelay": True, "minimumHealthyEntities": 0}}],
    },
})
body = {
    "apiVersion": "platform.delivery/v1alpha1",
    "kind": "EnvironmentDeployment",
    "environment": {"id": "dev", "generation": generation},
    "releaseChannels": [
        {"name": "DEV", "description": "Development"},
        {"name": "STAGING", "description": "Staging"},
    ],
    "releases": releases,
    "productSettings": settings,
    "entities": entities,
}
json.dump(body, open(sys.argv[1], "w"))
PY

curl -fsS -X POST "${hub}/v1/plans" \
  -H 'content-type: application/json' \
  -H 'accept: application/json' \
  -H 'X-Change-Title: Generate hello-http' \
  -H 'X-Change-Description: a small page that says hello' \
  --data @"${state_dir}/generation-plan.json" >"${state_dir}/generation-plan-response.json"
change_id="$(python3 -c 'import json; print(json.load(open("'"${state_dir}"'/generation-plan-response.json"))["changeRequestId"])')"

curl -fsS -X POST "${hub}/v1/change-requests/${change_id}/approve" \
  -H 'content-type: application/json' \
  -H 'X-Platform-Subject: reviewer' \
  -H 'X-Platform-Groups: reviewers' \
  --data '{"rationale":"generated application accepted"}' >/dev/null

lease_file="${state_dir}/generation-lease.json"
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

python3 - "${lease_file}" "${state_dir}/generation-finish.json" <<'PY'
import json, sys
lease = json.load(open(sys.argv[1]))
steps = lease["plan"]["steps"]
entities, tasks = [], []
for index, step in enumerate(steps, start=1):
    started = f"2026-11-14T12:00:{index-1:02d}Z"
    finished = f"2026-11-14T12:00:{index:02d}Z"
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
        "sequence": index, "phase": "APPLY", "status": "SUCCEEDED",
        "installationId": step["installationId"], "namespace": step["namespace"],
        "startedAt": started, "finishedAt": finished,
    })
json.dump({
    "status": "SUCCEEDED",
    "execution": {"startedAt": "2026-11-14T12:00:00Z", "finishedAt": f"2026-11-14T12:00:{len(steps):02d}Z", "tasks": tasks},
    "entities": entities,
}, open(sys.argv[2], "w"))
PY
curl -fsS -X POST "${hub}/v1/plans/${plan_id}/finish" \
  -H 'content-type: application/json' \
  -H 'X-Platform-Environment-ID: dev' \
  -H 'X-Platform-Agent-ID: spoke-dev' \
  -H "X-Platform-Lease-Token: ${token}" \
  --data @"${state_dir}/generation-finish.json" >/dev/null

now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
hour_ago="$(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)"
curl -fsS -X POST "${hub}/v1/promotions/evaluations" \
  -H 'content-type: application/json' \
  --data "$(cat <<JSON
{
  "release": {
    "product": "app.example:hello-http",
    "version": "0.1.0",
    "channels": ["DEV"],
    "chart": {"repository": "127.0.0.1:5000/hello-http-chart", "tag": "0.1.0", "digest": "${chart_digest}"},
    "images": [{"repository": "127.0.0.1:5000/hello-http", "tag": "0.1.0", "digest": "${image_digest}"}]
  },
  "fromChannel": "DEV",
  "toChannel": "STAGING",
  "policy": {"noDelay": true, "minimumHealthyEntities": 1, "maximumUnhealthyEntities": 0},
  "evidence": [{
    "environment": "dev",
    "installationId": "hello-http",
    "health": "HEALTHY",
    "healthySince": "${hour_ago}",
    "observedAt": "${now}"
  }]
}
JSON
)" >"${state_dir}/generation-promotion.json"

curl -fsS -X PUT "${hub}/v1/export-pipelines/hello-http" \
  -H 'content-type: application/json' \
  --data '{"id":"hello-http","displayName":"Hello HTTP","buildStrategy":"SNAPSHOT","selections":[{"type":"ENVIRONMENT","ids":["dev"],"releaseStrategy":"ALL","releaseChannels":["DEV"]}]}' >/dev/null
curl -fsS -X POST "${hub}/v1/export-pipelines/hello-http/build" >"${state_dir}/generation-build.json"
bundle_id="$(python3 -c 'import json; print(json.load(open("'"${state_dir}"'/generation-build.json"))["bundles"][0]["manifest"]["id"])')"

printf 'packaging run %s\nbundle %s\nchange request %s\n' "${run_id}" "${bundle_id}" "${change_id}" | tee "${state_dir}/generation-ids.txt"
