#!/usr/bin/env bash
# Start a local Hub, one packaging worker, and one sample application, then
# record a packaging run, a signed bundle, a change request, its approval,
# a promotion from DEV to STAGING, and a recall. Running this again clears
# the previous copy first.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
"${root}/scripts/build-console.sh"
state_dir="${CONDUIT_SEED_DIR:-/tmp/conduit-seed}"
"${root}/scripts/demo-reset.sh"

mkdir -p "${state_dir}/data" "${state_dir}/packaging"
pid_file="${state_dir}/pids"
: >"${pid_file}"

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "Docker is required to publish the sample image and chart" >&2
  exit 1
fi
# shellcheck source=SCRIPTDIR/lib/demo-docker-network.sh
source "${root}/scripts/lib/demo-docker-network.sh"
demo_prepare_docker_network
registry_image="registry@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"
demo_start_registry "${registry_image}"
for _ in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:5000/v2/ >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl -fsS http://127.0.0.1:5000/v2/ >/dev/null 2>&1; then
  echo "Registry did not answer on 127.0.0.1:5000" >&2
  docker logs conduit-seed-registry 2>&1 | tail -20 >&2 || true
  exit 1
fi

publish_image() {
  local version="$1" body="$2" dir
  dir="${state_dir}/image-${version}"
  mkdir -p "${dir}"
  printf '%s\n' "${body}" >"${dir}/app.txt"
  cat >"${dir}/Dockerfile" <<'EOF'
FROM scratch
COPY app.txt /app.txt
EOF
  demo_docker_build "127.0.0.1:5000/sample-app:${version}" "${dir}"
  docker push "127.0.0.1:5000/sample-app:${version}" >&2
  docker image inspect "127.0.0.1:5000/sample-app:${version}" --format '{{range .RepoDigests}}{{println .}}{{end}}' |
    sed -n 's/.*@\(sha256:[0-9a-f]\{64\}\)$/\1/p' | head -1
}

python3 - "${state_dir}" <<'PY'
import hashlib, io, json, sys, tarfile, urllib.request
state, registry = sys.argv[1], "http://127.0.0.1:5000"

def push_chart(name, version, memory):
    files = {
        f"{name}/Chart.yaml": f"apiVersion: v2\nname: {name}\nversion: {version}\n",
        f"{name}/values.yaml": f"memory: {memory}\n",
        f"{name}/templates/configmap.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: sample-app\ndata:\n  memory: {{ .Values.memory }}\n",
    }
    blob_file = io.BytesIO()
    with tarfile.open(fileobj=blob_file, mode="w:gz") as archive:
        for path, text in files.items():
            raw = text.encode()
            info = tarfile.TarInfo(path)
            info.size = len(raw)
            archive.addfile(info, io.BytesIO(raw))
    blob = blob_file.getvalue()
    config = json.dumps({"name": name, "version": version, "memory": memory}).encode()
    def upload(payload, digest):
        request = urllib.request.Request(f"{registry}/v2/{name}/blobs/uploads/", method="POST")
        with urllib.request.urlopen(request) as response:
            location = response.headers["Location"]
        if location.startswith("/"):
            location = "http://127.0.0.1:5000" + location
        joiner = "&" if "?" in location else "?"
        put = urllib.request.Request(location + joiner + "digest=" + digest, data=payload, method="PUT")
        put.add_header("Content-Type", "application/octet-stream")
        urllib.request.urlopen(put).read()
    blob_digest = "sha256:" + hashlib.sha256(blob).hexdigest()
    config_digest = "sha256:" + hashlib.sha256(config).hexdigest()
    upload(blob, blob_digest)
    upload(config, config_digest)
    manifest = json.dumps({
        "schemaVersion": 2,
        "mediaType": "application/vnd.oci.image.manifest.v1+json",
        "config": {"mediaType": "application/vnd.cncf.helm.config.v1+json", "digest": config_digest, "size": len(config)},
        "layers": [{"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": blob_digest, "size": len(blob)}],
    }, separators=(",", ":")).encode()
    digest = "sha256:" + hashlib.sha256(manifest).hexdigest()
    put = urllib.request.Request(f"{registry}/v2/{name}/manifests/{version}", data=manifest, method="PUT")
    put.add_header("Content-Type", "application/vnd.oci.image.manifest.v1+json")
    urllib.request.urlopen(put).read()
    return digest

charts = {
    "0.1.0": push_chart("sample-app-chart", "0.1.0", "256Mi"),
    "0.2.0": push_chart("sample-app-chart", "0.2.0", "512Mi"),
}
open(f"{state}/charts.json", "w").write(json.dumps(charts))
print(charts)
PY

image_010="$(publish_image 0.1.0 'sample-app 0.1.0')"
image_020="$(publish_image 0.2.0 'sample-app 0.2.0')"
chart_010="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["0.1.0"])' "${state_dir}/charts.json")"
chart_020="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["0.2.0"])' "${state_dir}/charts.json")"
for digest in "${image_010}" "${image_020}" "${chart_010}" "${chart_020}"; do
  if [[ ! "${digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "registry digest was not a sha256: ${digest}" >&2
    exit 1
  fi
done
printf 'image_010=%s\nimage_020=%s\nchart_010=%s\nchart_020=%s\n' \
  "${image_010}" "${image_020}" "${chart_010}" "${chart_020}" >"${state_dir}/digests.env"
echo "published ${image_020} ${chart_020}"

if ! "${root}/scripts/fetch-temporal-cli.sh" >/dev/null; then
  echo "Temporal CLI could not be fetched" >&2
  exit 1
fi

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
  PACKAGING_FIXTURE_DIR="${state_dir}/packaging/fixture" \
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

fixture="${state_dir}/packaging/fixture"
for _ in $(seq 1 90); do
  if [[ -f "${fixture}/source.txt" ]]; then
    break
  fi
  sleep 1
done
if [[ ! -f "${fixture}/source.txt" ]]; then
  echo "Packaging worker did not materialize its fixture" >&2
  tail -30 "${state_dir}/worker.log" >&2 || true
  exit 1
fi
mkdir -p "${fixture}/sources"
write_run_source() {
  local path="$1" proposal="$2" rationale="$3" request="$4" limit="$5" chart="$6"
  cat >"${path}" <<EOF
name: sample-app
kind: fixture
proposal: ${proposal}
rationale: ${rationale}
memory-request: ${request}
memory: ${limit}
chart-values: memory: ${chart}
resources: limits.memory=${limit} requests.memory=${request}
image-ref: 127.0.0.1:5000/sample-app:0.2.0
image-digest: ${image_020}
EOF
}
write_run_source "${fixture}/sources/sample-app-1.txt" \
  "memory limit 512Mi" "the sample process is killed at 256Mi" "256Mi" "512Mi" "512Mi"
write_run_source "${fixture}/sources/sample-app-blocked.txt" \
  "memory limit 8Gi" "unbounded growth" "512Mi" "8Gi" "512Mi"
go run ./cmd/packaging-workflow run sample-app-1 --address 127.0.0.1:7233 \
  --source-file "${fixture}/sources/sample-app-1.txt"
for _ in $(seq 1 90); do
  phase="$(curl -fsS "${hub}/v1/packaging-runs/sample-app-1" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("phase",""))' || true)"
  if [[ "${phase}" == "review" || "${phase}" == "blocked" || "${phase}" == "complete" ]]; then
    break
  fi
  sleep 1
done
if [[ "${phase}" != "review" && "${phase}" != "blocked" && "${phase}" != "complete" ]]; then
  echo "packaging run sample-app-1 did not leave validation (phase ${phase})" >&2
  exit 1
fi
go run ./cmd/packaging-workflow run sample-app-blocked --address 127.0.0.1:7233 \
  --source-file "${fixture}/sources/sample-app-blocked.txt"

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
api PUT /v1/teams/Operator "$(cat <<'JSON'
{"name":"Operator","displayName":"Operators","adminUsers":["local-developer"],"memberUsers":["local-developer"]}
JSON
)" >/dev/null

api PUT /v1/approval-policies/release-review "$(cat <<'JSON'
{"id":"release-review","displayName":"Release review","enabled":true,"expression":"type != \"\" ? require_team(\"release-reviewers\", \"release review\") : no_op()"}
JSON
)" >/dev/null

plan_file="${state_dir}/plan.json"
python3 - "${state_dir}/digests.env" "${state_dir}/plan-body.json" <<'PY'
import json, sys
values = {}
for line in open(sys.argv[1]):
    key, value = line.strip().split("=", 1)
    values[key] = value
def release(version, chart, image):
    return {
        "product": "app.example:sample-app",
        "version": version,
        "channels": ["DEV"],
        "chart": {"repository": "127.0.0.1:5000/sample-app-chart", "tag": version, "digest": chart},
        "images": [{"repository": "127.0.0.1:5000/sample-app", "tag": version, "digest": image}],
    }
body = {
  "apiVersion": "platform.delivery/v1alpha1",
  "kind": "EnvironmentDeployment",
  "environment": {"id": "dev", "generation": "0123456789abcdef"},
  "releaseChannels": [
    {"name": "DEV", "description": "Development"},
    {"name": "STAGING", "description": "Staging"},
  ],
  "releases": [
    release("0.1.0", values["chart_010"], values["image_010"]),
    release("0.2.0", values["chart_020"], values["image_020"]),
  ],
  "recalls": [
    {
      "id": "rcl-sample-010",
      "product": "app.example:sample-app",
      "startingVersion": "0.1.0",
      "singleVersion": True,
      "reason": "sample release failed its release check",
      "strategy": {"type": "ROLL_FORWARD"}
    }
  ],
  "productSettings": [
    {
      "product": "app.example:sample-app",
      "promotionPipeline": {
        "enabled": True,
        "stages": [{"fromChannel": "DEV", "toChannel": "STAGING", "policy": {"noDelay": True, "minimumHealthyEntities": 0}}]
      }
    }
  ],
  "entities": [
    {
      "name": "sample-app",
      "product": "app.example:sample-app",
      "version": "0.2.0",
      "namespace": "sample",
      "helmValues": {"resources": {"limits": {"memory": "512Mi"}}}
    }
  ],
}
json.dump(body, open(sys.argv[2], "w"))
PY
curl -fsS -X POST "${hub}/v1/plans" \
  -H 'content-type: application/json' \
  -H 'accept: application/json' \
  -H 'X-Change-Title: Raise memory limit to 512Mi' \
  -H 'X-Change-Description: the sample process is killed at 256Mi' \
  --data @"${state_dir}/plan-body.json" >"${plan_file}"

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
    "chart": {"repository": "127.0.0.1:5000/sample-app-chart", "tag": "0.2.0", "digest": "${chart_020}"},
    "images": [{"repository": "127.0.0.1:5000/sample-app", "tag": "0.2.0", "digest": "${image_020}"}]
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
components = preview.get("sbom") or []
digests = {item.get("digest") for item in components}
values = dict(line.strip().split("=", 1) for line in open("${state_dir}/digests.env") if "=" in line)
if values["image_020"] not in digests or values["chart_020"] not in digests:
    raise SystemExit(f"bundle sbom does not carry the published 0.2.0 digests: {components}")
if any(digest.endswith("a" * 16) or digest.endswith("b" * 16) for digest in digests):
    raise SystemExit(f"bundle sbom still has a placeholder digest: {components}")
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
blocked=""
for _ in $(seq 1 60); do
  blocked="$(curl -fsS "${hub}/v1/packaging-runs/sample-app-blocked" | python3 -c 'import json,sys; body=json.load(sys.stdin); print(body.get("phase",""), body.get("outcome",""))')"
  if [[ "${blocked}" == blocked* ]]; then
    break
  fi
  sleep 1
done
if [[ "${blocked}" != blocked* ]]; then
  echo "packaging run sample-app-blocked is ${blocked}, want blocked" >&2
  exit 1
fi

echo "Hub ${hub}"
echo "console ${hub}/console/#area-packaging"
echo "packaging ${hub}/console/#area-packaging"
echo "releases ${hub}/console/#area-releases"
echo "change requests ${hub}/console/#area-changes"
echo "intelligence ${hub}/console/#area-intelligence"
echo "fleet ${hub}/console/#area-fleet"
echo "audit ${hub}/console/#area-audit"
echo "packaging run sample-app-1 is in review"
echo "bundle ${bundle_id}"
echo "change request ${change_id} approved"
