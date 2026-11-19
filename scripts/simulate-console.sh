#!/usr/bin/env bash
# Drive the operator console against a real Hub.
#
# Why this exists beside web/src/test/e2e, which already drives the console:
# that suite intercepts /v1 and serves fixtures, and says so in its own harness
# -- it establishes that the console renders correctly GIVEN a response, never
# that the Hub produces that response. A field the Hub renames, a query
# parameter the console sends that the Hub does not read, a nil slice the Go
# encoder writes as null where the fixture wrote [], an authorization filter
# that empties a list: none of those can fail there, and all of them ship.
#
# So this builds the Hub, runs it, seeds it through its own API, and points the
# browser at the console the Hub itself serves from internal/consoleui/dist --
# the embedded bundle, not a Vite preview. There is no stub anywhere in the
# loop.
#
# Run locally when the transport between console and Hub is in question.
# The separate Native console journeys workflow also invokes this harness;
# scripts/test-console.sh retains its independent mocked browser suite.
#
# The scenarios still seed a Hub. Every scenario opens the same shell check: the
# embedded bundle hydrates the tenant bootstrap and says the local Codex bridge
# is absent. The healthy scenario also walks the release journey through the
# operator workbench (release-journey.spec.ts), approving the seeded change
# request as a second person, and takes assistant turns against the Hub's own
# turn loop and thread store (assistant-turns.spec.ts) with a stand-in model.
#   ./scripts/simulate-console.sh                      # embedded assistant shell
#   ./scripts/simulate-console.sh --with-workspace-git  # local Git history/fork
#   ./scripts/simulate-console.sh --keep               # leave the Hub running
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
keep=0
simulate_agent=1
with_packaging=0
with_workspace_git=0
with_planning=0
# Which desired state to seed, and so which branch of the simulator the Agent
# takes. Each scenario has its own spec file because their assertions contradict
# one another by design: a Hub carrying a failed Plan cannot also satisfy
# "nothing needs attention".
scenario=healthy
while [ "$#" -gt 0 ]; do
  case "$1" in
    --keep) keep=1; shift ;;
    --scenario)
      scenario="${2:-}"
      case "${scenario}" in
        healthy|failure|degraded) ;;
        *) echo "simulate: unknown scenario '${scenario}' (healthy, failure, degraded)" >&2; exit 2 ;;
      esac
      shift 2 ;;
    # Without an Agent the fleet never reports. Kept as an option because it is
    # the state the console was in before one existed, and a spec that asserts
    # on staleness needs it.
    --no-agent) simulate_agent=0; shift ;;
    --with-packaging) with_packaging=1; shift ;;
    --with-workspace-git) with_workspace_git=1; shift ;;
    --with-planning) with_planning=1; shift ;;
    *) break ;;
  esac
done

if [ "${with_packaging}" -eq 1 ] && [ "${scenario}" != healthy ]; then
  echo "simulate: --with-packaging requires the healthy scenario" >&2
  exit 2
fi

if [ "${with_workspace_git}" -eq 1 ] && [ "${scenario}" != healthy ]; then
  echo "simulate: --with-workspace-git requires the healthy scenario" >&2
  exit 2
fi
if [ "${with_planning}" -eq 1 ] && [ "${scenario}" != healthy ]; then
  echo "simulate: --with-planning requires the healthy scenario" >&2
  exit 2
fi

port="${CONDUIT_SIMULATION_PORT:-8099}"
hub_url="http://127.0.0.1:${port}"
work="$(mktemp -d)"
# The stand-in model listens beside the Hub. See scripts/simulate/fake-model.mjs.
model_port="${CONDUIT_SIMULATION_MODEL_PORT:-$((port + 1))}"
hub_pid=""
agent_pid=""
model_pid=""
source_pid=""
planning_repo=""
planning_source_host=""
packaging_pid=""
temporal_pid=""
packaging_run=""
evidence_dir="${CONDUIT_SIMULATION_EVIDENCE_DIR:-}"
workspace_git_root=""
if [ "${with_workspace_git}" -eq 1 ]; then workspace_git_root="${work}/workspaces"; fi

cleanup() {
  local status=$?
  if [ "${keep}" -eq 0 ]; then
    for pid in "${agent_pid}" "${hub_pid}" "${model_pid}" "${source_pid}" "${packaging_pid}" "${temporal_pid}"; do
      [ -n "${pid}" ] || continue
      kill "${pid}" 2>/dev/null || true
      wait "${pid}" 2>/dev/null || true
    done
  fi
  if [ "${status}" -ne 0 ]; then
    for log in hub agent model source packaging temporal; do
      [ -s "${work}/${log}.log" ] || continue
      echo "--- ${log} log ---" >&2
      tail -40 "${work}/${log}.log" >&2
    done
  fi
  if [ -n "${evidence_dir}" ]; then
    mkdir -p "${evidence_dir}"
    for log in hub agent model source packaging temporal; do
      if [ -f "${work}/${log}.log" ]; then cp "${work}/${log}.log" "${evidence_dir}/${log}.log"; fi
    done
    if [ -f "${work}/packaging-status.json" ]; then cp "${work}/packaging-status.json" "${evidence_dir}/packaging-status.json"; fi
  fi
  [ "${keep}" -eq 0 ] && rm -rf "${work}"
  return "${status}"
}
trap cleanup EXIT

# The loopback Hub must not be reached through an ambient egress proxy, which is
# present in some development sandboxes and swallows the connection silently.
export NO_PROXY="127.0.0.1,localhost,${NO_PROXY:-}"
export no_proxy="${NO_PROXY}"

# Refuse a port someone else is on. Without this the readiness probe below is
# satisfied by whatever Hub is already listening, the simulation seeds a stranger
# and asserts against it, and the run reports on state this script never created.
# That happened while this was being written and read as a seeding bug.
if curl -sf --noproxy '*' -o /dev/null --max-time 2 "${hub_url}/console/" 2>/dev/null; then
  echo "simulate: something is already serving a console on ${hub_url}" >&2
  echo "simulate: stop it, or set CONDUIT_SIMULATION_PORT to a free port" >&2
  exit 1
fi

echo "simulate: building the console"
"${repo_root}/scripts/build-console.sh"
echo "simulate: building the Hub"
go build -o "${work}/hub" "${repo_root}/cmd/platform-delivery"

# Opt-in recorded-fixture packaging worker, using the real native workflow and
# Temporal status/history reader. No approval is sent and no artifact is published.
if [ "${with_packaging}" -eq 1 ]; then
  cli="${CONDUIT_TEMPORAL_CLI:-${repo_root}/.cache/temporal/temporal}"
  [ -x "${cli}" ] || { echo "simulate: --with-packaging requires the verified pinned Temporal CLI" >&2; exit 1; }
  go build -o "${work}/packaging-workflow" "${repo_root}/cmd/packaging-workflow"
  "${work}/packaging-workflow" --cli "${cli}" --db "${work}/temporal.db" start-server >"${work}/temporal.log" 2>&1 &
  temporal_pid=$!
  packaging_address=""
  for _ in $(seq 1 120); do
    packaging_address="$(awk '/^frontend 127\.0\.0\.1:[0-9]+ ui http:\/\/127\.0\.0\.1:/ { print $2; exit }' "${work}/temporal.log")"
    [ -z "${packaging_address}" ] || break
    kill -0 "${temporal_pid}" 2>/dev/null || { echo "simulate: Temporal exited during startup" >&2; exit 1; }
    sleep 0.5
  done
  [ -n "${packaging_address}" ] || { echo "simulate: Temporal did not report its loopback frontend" >&2; exit 1; }
  "${work}/packaging-workflow" --address "${packaging_address}" --state-dir "${work}/packaging-state" worker >"${work}/packaging.log" 2>&1 &
  packaging_pid=$!
  packaging_run="packaging-feedback-1"
  "${work}/packaging-workflow" --address "${packaging_address}" run "${packaging_run}"
  packaging_ready=0
  for _ in $(seq 1 120); do
    if "${work}/packaging-workflow" --address "${packaging_address}" status "${packaging_run}" >"${work}/packaging-status.json" 2>/dev/null &&
      python3 -c 'import json,sys; row=json.load(open(sys.argv[1])); sys.exit(0 if row.get("phase")=="review" and row.get("runId")==sys.argv[2] and not row.get("planId") else 1)' "${work}/packaging-status.json" "${packaging_run}"; then
      packaging_ready=1
      break
    fi
    kill -0 "${packaging_pid}" 2>/dev/null || { echo "simulate: packaging worker exited during startup" >&2; exit 1; }
    sleep 0.5
  done
  [ "${packaging_ready}" -eq 1 ] || { echo "simulate: packaging workflow did not reach unapproved review" >&2; exit 1; }
  export PACKAGING_TEMPORAL_ADDRESS="${packaging_address}"
  echo "simulate: real recorded-fixture packaging workflow ${packaging_run} is waiting for review"
fi


# The assistant needs a model to take a turn at all, and a simulation cannot
# call a real one. This is a loopback script that calls one read tool and then
# answers; everything around it -- the turn loop, the tool, the audit entries,
# the thread store, the event stream -- is the Hub's own. Plain http is accepted
# without an opt-in because the address is loopback.
echo "simulate: starting the stand-in model on 127.0.0.1:${model_port}"
if [ "${with_planning}" -eq 1 ]; then
  mkdir -p "${work}/sources"
  git init --quiet --initial-branch=main "${work}/planning-source"
  printf '%s\n' '# Local planning cancellation fixture' >"${work}/planning-source/README.md"
  git -C "${work}/planning-source" add README.md
  git -C "${work}/planning-source" -c user.name=Fixture -c user.email=fixture@example.invalid commit --quiet -m 'test: seed planning cancellation fixture'
  git clone --quiet --bare "${work}/planning-source" "${work}/sources/planning-cancel.git"
  git -C "${work}/sources/planning-cancel.git" update-server-info
  python3 -u -m http.server 0 --bind 127.0.0.1 --directory "${work}/sources" >"${work}/source.log" 2>&1 &
  source_pid=$!
  source_ready=0
  for _ in $(seq 1 40); do
    kill -0 "${source_pid}" 2>/dev/null || { echo "simulate: planning source server exited" >&2; exit 1; }
    source_port="$(awk '/^Serving HTTP on 127\.0\.0\.1 port [0-9]+ / { print $6; exit }' "${work}/source.log")"
    if [ -n "${source_port}" ]; then source_ready=1; break; fi
    sleep 0.25
  done
  [ "${source_ready}" -eq 1 ] || { echo "simulate: planning source server unavailable" >&2; exit 1; }
  planning_source_host="127.0.0.1:${source_port}"
  planning_repo="http://${planning_source_host}/planning-cancel.git"
  curl -fsS --noproxy '*' --max-time 2 "${planning_repo}/HEAD" >"${work}/source-head.txt"
  grep -qx 'ref: refs/heads/main' "${work}/source-head.txt"
fi
CONDUIT_SIMULATION_PLANNING_REPO="${planning_repo}" node "${repo_root}/scripts/simulate/fake-model.mjs" "${model_port}" >"${work}/model.log" 2>&1 &
model_pid=$!
for _ in $(seq 1 40); do
  grep -q "listening" "${work}/model.log" 2>/dev/null && break
  kill -0 "${model_pid}" 2>/dev/null || { echo "simulate: the stand-in model exited during startup" >&2; exit 1; }
  sleep 0.25
done
grep -q "listening" "${work}/model.log" ||
  { echo "simulate: the stand-in model never listened" >&2; exit 1; }

echo "simulate: starting it on ${hub_url}"
# Insecure identity headers are what make a browser-driven simulation possible
# at all: the console has no OIDC provider here, so the operator it acts as is
# named by a header the harness sets. The signing opt-in is the same kind of
# declaration for the same kind of reason: there is no signing Secret here, and
# a Hub that has not said it is a development Hub refuses to start rather than
# signing Bundles as a signer nobody trusts. This is why the script is not a
# gate -- it configures a Hub no deployment should run.
#
# The anonymous burst is raised because every spec opens a fresh browser
# context, which downloads the whole embedded bundle again from one loopback
# address within a second or two. The default bucket (60, refilled at 10 a
# second) is shaped for people, and the suite drains it: the last spec's page
# load was answered 429 once the assistant specs were added in front of it.
LISTEN_ADDRESS="127.0.0.1:${port}" \
  RATE_LIMIT_ANONYMOUS_BURST=600 \
  DATA_DIRECTORY="${work}/data" \
  DEVWORKSPACE_GIT_ROOT="${workspace_git_root}" \
  ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_AGENT_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY=true \
  PLATFORM_BOOTSTRAP_ADMINISTRATORS=proposer,operator \
  CONDUIT_MODEL_BASE_URL="http://127.0.0.1:${model_port}/v1" \
  CONDUIT_MODEL_NAME=simulated-model \
  CONDUIT_SOURCE_ALLOWED_HOSTS="${planning_source_host:-${CONDUIT_SOURCE_ALLOWED_HOSTS:-}}" \
  "${work}/hub" >"${work}/hub.log" 2>&1 &
hub_pid=$!

for _ in $(seq 1 40); do
  if curl -sf --noproxy '*' -o /dev/null "${hub_url}/console/"; then
    break
  fi
  kill -0 "${hub_pid}" 2>/dev/null || { echo "simulate: the Hub exited during startup" >&2; exit 1; }
  sleep 0.5
done
curl -sf --noproxy '*' -o /dev/null "${hub_url}/console/" ||
  { echo "simulate: the Hub never served the console" >&2; exit 1; }

# Visibility is eventually indexed: require the actual Hub catalog to expose
# the seeded workflow before the browser assertions start.
if [ "${with_packaging}" -eq 1 ]; then
  packaging_visible=0
  for _ in $(seq 1 120); do
    if curl -fsS --noproxy '*' -H 'X-Platform-Subject: operator' "${hub_url}/v1/packaging-runs" >"${work}/packaging-index.json" &&
      python3 -c 'import json,sys; rows=json.load(open(sys.argv[1])).get("items",[]); sys.exit(0 if any(row.get("runId")==sys.argv[2] for row in rows) else 1)' "${work}/packaging-index.json" "${packaging_run}"; then
      packaging_visible=1
      break
    fi
    sleep 0.5
  done
  [ "${packaging_visible}" -eq 1 ] || { echo "simulate: packaging workflow was not visible through the Hub" >&2; exit 1; }
fi

# Seeded as "proposer", while the browser acts as "operator". They have to be
# different people: the Hub refuses the author of a Change Request approving it,
# so a simulation that proposed and approved as one subject could never exercise
# the approval the console exists to perform. Both are bootstrap administrators
# so neither needs a grant seeded before it can act.
echo "simulate: seeding through the Hub's own API"
plan_id="$(CONDUIT_HUB_URL="${hub_url}" CONDUIT_SUBJECT=proposer CONDUIT_SCENARIO="${scenario}" \
  go run "${repo_root}/scripts/simulate/seed.go")"
echo "simulate: seeded Plan ${plan_id}"

# The Agent is what makes the fleet move. Without it nothing reports, every
# installation reads UNKNOWN and stale, and no Plan gets past AWAITING_APPROVAL
# -- reported state reaches the Hub only through a lease token minted for an
# approved Plan, so no amount of seeding substitutes for one.
#
# It is a separate binary from the shipped Agent on purpose; see its own doc
# comment. Nothing here is a mode of cmd/platform-spoke-agent.
if [ "${simulate_agent}" -eq 1 ]; then
  echo "simulate: starting the simulated Agent"
  go build -o "${work}/simulated-agent" "${repo_root}/cmd/platform-simulated-agent"
  CONTROL_PLANE_URL="${hub_url}" ENVIRONMENT=dev-use2 \
    "${work}/simulated-agent" >"${work}/agent.log" 2>&1 &
  agent_pid=$!
fi

if [ "${keep}" -eq 1 ]; then
  echo "simulate: Hub left running at ${hub_url}/console/ (pid ${hub_pid}, data ${work})"
  echo "simulate: stop it with: kill ${hub_pid} ${agent_pid} ${model_pid} ${source_pid} ${packaging_pid} ${temporal_pid}"
  exit 0
fi

# Explicit rather than derived from the scenario name: the mapping is the thing
# a reader needs, and a scenario whose specs silently did not exist would pass.
case "${scenario}" in
  healthy) specs="assistant.spec.ts assistant-turns.spec.ts release-journey.spec.ts release-write-journey.spec.ts organization-governance.spec.ts intelligence-feedback.spec.ts workspace-metadata.spec.ts" ;;
  failure|degraded) specs=assistant.spec.ts ;;
  *) echo "simulate: unknown scenario '${scenario}'" >&2; exit 1 ;;
esac
if [ "${with_workspace_git}" -eq 1 ]; then
  specs="${specs/workspace-metadata.spec.ts/workspace-git.spec.ts}"
fi
if [ "${with_packaging}" -eq 1 ]; then specs="${specs} packaging-feedback.spec.ts"; fi
if [ "${with_planning}" -eq 1 ]; then specs="${specs} planning-cancellation.spec.ts"; fi
for spec in ${specs}; do
  [ -f "${repo_root}/web/src/test/live/${spec}" ] ||
    { echo "simulate: scenario '${scenario}' has no spec file ${spec}" >&2; exit 1; }
done

# shellcheck disable=SC2086 # specs is a space-separated list of file names
CONDUIT_HUB_URL="${hub_url}/console/" CONDUIT_SIMULATED_PLAN="${plan_id}" CONDUIT_SIMULATED_PACKAGING_RUN="${packaging_run}" \
  CONDUIT_SIMULATED_PLANNING_REPO="${planning_repo}" CONDUIT_SIMULATED_MODEL_ORIGIN="http://127.0.0.1:${model_port}" \
  pnpm --dir "${repo_root}/web" exec playwright test --config playwright.live.config.ts ${specs} "$@"
