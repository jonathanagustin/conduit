#!/usr/bin/env bash
# Drive the governed packaging path.
# With PACKAGING_REPO_URL unset, run the deterministic fixture test.
# With PACKAGING_REPO_URL set, start a Hub and package that repository.
# CONDUIT_MODEL_CMD defaults to the deterministic agent. Point it at a live
# model CLI to write the Dockerfile with that CLI instead.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "docker is required and its daemon must be running" >&2
  exit 69
fi
if ! command -v zarf >/dev/null 2>&1 && [[ -z "${ZARF_BIN:-}" ]]; then
  echo "zarf is required on PATH or as ZARF_BIN. The qualified binary is v0.82.0." >&2
  exit 69
fi

if [[ -z "${PACKAGING_REPO_URL:-}" && "${PACKAGING_HUB_ONLY:-}" != "1" ]]; then
  CONDUIT_IMAGE_BUILDER=docker CONDUIT_PACKAGING_BUILD=scratch \
    go test -count=1 -timeout 20m -run TestGovernedOnboardPackagingFromSource ./internal/hub/composition
  exit 0
fi

state="${PACKAGING_STATE_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/conduit-packaging.XXXXXX")}"
port="${PACKAGING_HUB_PORT:-8081}"
hub="http://127.0.0.1:${port}"
mkdir -p "${state}/workspaces" "${state}/data"
go build -o "${state}/packaging-workflow" ./cmd/packaging-workflow
model="${CONDUIT_MODEL_CMD:-${state}/packaging-workflow agent-exec}"
build_mode="${CONDUIT_PACKAGING_BUILD:-}"
if [[ -z "${build_mode}" && "${PACKAGING_REPO_URL}" == *excalidraw* ]]; then
  build_mode="source"
fi
if [[ -z "${build_mode}" ]]; then
  build_mode="scratch"
fi

nohup env \
  LISTEN_ADDRESS="127.0.0.1:${port}" \
  DATA_DIRECTORY="${state}/data" \
  ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_AGENT_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY=true \
  PLATFORM_BOOTSTRAP_ADMINISTRATORS=alice \
  DEVWORKSPACE_GIT_ROOT="${state}/workspaces" \
  CONDUIT_IMAGE_BUILDER=docker \
  CONDUIT_PACKAGING_BUILD="${build_mode}" \
  CONDUIT_DOCKER_BUILD_NETWORK="${CONDUIT_DOCKER_BUILD_NETWORK:-host}" \
  CONDUIT_MODEL_CMD="${model}" \
  CONDUIT_HTTP_WRITE_TIMEOUT="${CONDUIT_HTTP_WRITE_TIMEOUT:-45m}" \
  go run ./cmd/platform-delivery >"${state}/hub.log" 2>&1 &
echo $! >"${state}/hub.pid"
cleanup() {
  if [[ -f "${state}/hub.pid" ]]; then
    kill "$(cat "${state}/hub.pid")" 2>/dev/null || true
  fi
}
trap cleanup EXIT

for _ in $(seq 1 120); do
  if curl -fsS "${hub}/version" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
if ! curl -fsS "${hub}/version" >/dev/null 2>&1; then
  echo "Hub did not answer ${hub}/version" >&2
  tail -40 "${state}/hub.log" >&2 || true
  exit 1
fi

if [[ "${PACKAGING_HUB_ONLY:-}" == "1" ]]; then
  echo "hub ${hub}"
  echo "state ${state}"
  trap - EXIT
  exit 0
fi

auth=(-H "X-Platform-Subject: alice" -H "content-type: application/json")
ref="${PACKAGING_REF:-main}"
body="$(REF="${ref}" PACKAGING_REPO_URL="${PACKAGING_REPO_URL}" python3 -c 'import json,os; print(json.dumps({"repoUrl": os.environ["PACKAGING_REPO_URL"], "ref": os.environ["REF"]}))')"
session="$(curl -fsS -X POST "${hub}/v1/planning-sessions" "${auth[@]}" -d "${body}")"
sid="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${session}")"
curl -fsS -X POST "${hub}/v1/planning-sessions/${sid}/turns" "${auth[@]}" -d '{"message":"Inspect the repository and prepare the package."}' >/dev/null
curl -fsS -X POST "${hub}/v1/planning-sessions/${sid}/answers" "${auth[@]}" -d '{"answers":[{"id":"node_version","value":"20"}]}' >/dev/null || true
curl -fsS -X POST "${hub}/v1/planning-sessions/${sid}/turns" "${auth[@]}" -d '{"message":"Write the packaging plan."}' >/dev/null
curl -fsS -X POST "${hub}/v1/planning-sessions/${sid}/accept" "${auth[@]}" >/dev/null
curl -fsS --max-time "${PACKAGING_COMPUTE_TIMEOUT:-2700}" -X POST "${hub}/v1/planning-sessions/${sid}/compute" "${auth[@]}" -d '{}' >/dev/null
ws="wsp_${sid}"
python3 - "${hub}" "${ws}" "${state}" <<'PY'
import json, sys, urllib.request
hub, ws, state = sys.argv[1:]
def get(path):
    req = urllib.request.Request(hub + path, headers={"X-Platform-Subject": "alice", "accept": "application/json"})
    with urllib.request.urlopen(req) as response:
        return json.load(response)
row = get("/v1/dev-workspaces/" + ws)
digest = row.get("artifactDigest") or ""
page = get("/v1/change-requests")
changes = page.get("changeRequests") or []
matched = [item for item in changes if digest and digest in (item.get("description") or "")]
print(json.dumps({
    "workspaceId": ws,
    "artifactDigest": digest,
    "validationOutcome": row.get("validationOutcome"),
    "changeRequestId": matched[0]["id"] if matched else "",
    "changeRequestStatus": matched[0].get("status") if matched else "",
    "stateDir": state,
}, indent=2))
if not digest.startswith("sha256:") or row.get("validationOutcome") != "PASS" or not matched:
    sys.exit(1)
PY
echo "state: ${state}"
echo "hub log: ${state}/hub.log"
trap - EXIT
