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
# Not a CI gate. It needs a Go toolchain, a browser, and a free port, and it is
# slower than the stubbed suite by the cost of a Hub build. Run it when the
# transport between console and Hub is what is in question.
#
# The scenarios still seed a Hub. Every scenario opens the same shell check: the
# embedded bundle hydrates the tenant bootstrap and says the local Codex bridge
# is absent. The healthy scenario also walks the release journey through the
# operator workbench (release-journey.spec.ts), approving the seeded change
# request as a second person.
#   ./scripts/simulate-console.sh                      # embedded assistant shell
#   ./scripts/simulate-console.sh --keep               # leave the Hub running
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
keep=0
simulate_agent=1
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
    *) break ;;
  esac
done

port="${CONDUIT_SIMULATION_PORT:-8099}"
hub_url="http://127.0.0.1:${port}"
work="$(mktemp -d)"
hub_pid=""
agent_pid=""

cleanup() {
  local status=$?
  if [ "${keep}" -eq 0 ]; then
    for pid in "${agent_pid}" "${hub_pid}"; do
      [ -n "${pid}" ] || continue
      kill "${pid}" 2>/dev/null || true
      wait "${pid}" 2>/dev/null || true
    done
  fi
  if [ "${status}" -ne 0 ]; then
    for log in hub agent; do
      [ -s "${work}/${log}.log" ] || continue
      echo "--- ${log} log ---" >&2
      tail -40 "${work}/${log}.log" >&2
    done
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

echo "simulate: starting it on ${hub_url}"
# Insecure identity headers are what make a browser-driven simulation possible
# at all: the console has no OIDC provider here, so the operator it acts as is
# named by a header the harness sets. The signing opt-in is the same kind of
# declaration for the same kind of reason: there is no signing Secret here, and
# a Hub that has not said it is a development Hub refuses to start rather than
# signing Bundles as a signer nobody trusts. This is why the script is not a
# gate -- it configures a Hub no deployment should run.
LISTEN_ADDRESS="127.0.0.1:${port}" \
  DATA_DIRECTORY="${work}/data" \
  ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_AGENT_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY=true \
  PLATFORM_BOOTSTRAP_ADMINISTRATORS=proposer,operator \
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
  echo "simulate: stop it with: kill ${hub_pid} ${agent_pid}"
  exit 0
fi

# Explicit rather than derived from the scenario name: the mapping is the thing
# a reader needs, and a scenario whose specs silently did not exist would pass.
case "${scenario}" in
  healthy) specs="assistant.spec.ts release-journey.spec.ts release-write-journey.spec.ts" ;;
  failure|degraded) specs=assistant.spec.ts ;;
  *) echo "simulate: unknown scenario '${scenario}'" >&2; exit 1 ;;
esac
for spec in ${specs}; do
  [ -f "${repo_root}/web/src/test/live/${spec}" ] ||
    { echo "simulate: scenario '${scenario}' has no spec file ${spec}" >&2; exit 1; }
done

# shellcheck disable=SC2086 # specs is a space-separated list of file names
CONDUIT_HUB_URL="${hub_url}/console/" CONDUIT_SIMULATED_PLAN="${plan_id}" \
  pnpm --dir "${repo_root}/web" exec playwright test --config playwright.live.config.ts ${specs} "$@"
