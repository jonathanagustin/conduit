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
#   ./scripts/simulate-console.sh              # build, seed, run the live specs
#   ./scripts/simulate-console.sh --keep        # leave the Hub running afterwards
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
keep=0
[ "${1:-}" = "--keep" ] && keep=1

port="${CONDUIT_SIMULATION_PORT:-8099}"
hub_url="http://127.0.0.1:${port}"
work="$(mktemp -d)"
hub_pid=""

cleanup() {
  local status=$?
  if [ -n "${hub_pid}" ] && [ "${keep}" -eq 0 ]; then
    kill "${hub_pid}" 2>/dev/null || true
    wait "${hub_pid}" 2>/dev/null || true
  fi
  if [ "${status}" -ne 0 ] && [ -s "${work}/hub.log" ]; then
    echo "--- Hub log ---" >&2
    tail -40 "${work}/hub.log" >&2
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
if curl -sf --noproxy '*' -o /dev/null --max-time 2 "${hub_url}/console/bootstrap.json" 2>/dev/null; then
  echo "simulate: something is already serving a console on ${hub_url}" >&2
  echo "simulate: stop it, or set CONDUIT_SIMULATION_PORT to a free port" >&2
  exit 1
fi

echo "simulate: building the Hub"
go build -o "${work}/hub" "${repo_root}/cmd/platform-delivery"

echo "simulate: starting it on ${hub_url}"
# Insecure identity headers are what make a browser-driven simulation possible
# at all: the console has no OIDC provider here, so the operator it acts as is
# named by a header the harness sets. This is why the script is not a gate --
# it configures a Hub no deployment should run.
LISTEN_ADDRESS="127.0.0.1:${port}" \
  DATA_DIRECTORY="${work}/data" \
  ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS=true \
  ALLOW_INSECURE_AGENT_IDENTITY_HEADERS=true \
  PLATFORM_BOOTSTRAP_ADMINISTRATORS=operator \
  "${work}/hub" >"${work}/hub.log" 2>&1 &
hub_pid=$!

for _ in $(seq 1 40); do
  if curl -sf --noproxy '*' -o /dev/null "${hub_url}/console/bootstrap.json"; then
    break
  fi
  kill -0 "${hub_pid}" 2>/dev/null || { echo "simulate: the Hub exited during startup" >&2; exit 1; }
  sleep 0.5
done
curl -sf --noproxy '*' -o /dev/null "${hub_url}/console/bootstrap.json" ||
  { echo "simulate: the Hub never served the console" >&2; exit 1; }

echo "simulate: seeding through the Hub's own API"
plan_id="$(CONDUIT_HUB_URL="${hub_url}" go run "${repo_root}/scripts/simulate/seed.go")"
echo "simulate: seeded Plan ${plan_id}"

if [ "${keep}" -eq 1 ]; then
  echo "simulate: Hub left running at ${hub_url}/console/ (pid ${hub_pid}, data ${work})"
  echo "simulate: stop it with: kill ${hub_pid}"
  exit 0
fi

CONDUIT_HUB_URL="${hub_url}/console/" CONDUIT_SIMULATED_PLAN="${plan_id}" \
  pnpm --dir "${repo_root}/web" exec playwright test --config playwright.live.config.ts "$@"
