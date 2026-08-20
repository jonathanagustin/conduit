#!/usr/bin/env bash
# Run the Trunk gate workflow itself -- not a local approximation of it -- with
# nektos/act.
#
# scripts/verify-local.sh runs every GATE the workflow runs, and reconciles the
# two lists in both directions so they cannot drift. That is a check on the
# gates. It is not a check on the WORKFLOW: the job graph, the `needs:` edges,
# `actions/checkout` pinning GATE_SHA, the pinned setup-go / setup-node / pnpm /
# Helm / Zarf / Hadolint installs, and the preflight's GitHub API work are all
# invisible to it. This runs those.
#
# It exists because GitHub-hosted runners are not always reachable. When Actions
# allocates no runner, every job reports `conclusion: failure` with zero steps,
# an empty runner name and no log blob -- a red check that no diff can turn
# green. This script is how a change gets workflow-level evidence anyway. It is
# NOT a substitute for the Trunk gate: verify-release-preconditions.sh reads the
# combined commit status of the head, and nothing here writes that status.
#
# Usage:
#   scripts/run-ci-locally.sh <pull-request-number> [job ...]
#
# With no job named it runs all four lanes. Each lane pulls in `preflight`
# through `needs:`, so the head authentication is verified four times; that is
# act's behaviour, not a bug here.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

readonly REPOSITORY="jonathanagustin/conduit"
readonly WORKFLOW=".github/workflows/platform-ci.yml"
readonly IMAGE="act-conduit:latest"
readonly BASE_IMAGE="catthehacker/ubuntu:act-latest"
readonly ALL_JOBS=(checks go console cli-matrix)

pull_request="${1:-}"
if [[ -z "${pull_request}" ]]; then
  echo "usage: ${BASH_SOURCE[0]} <pull-request-number> [job ...]" >&2
  exit 2
fi
shift
jobs=("$@")
[[ ${#jobs[@]} -eq 0 ]] && jobs=("${ALL_JOBS[@]}")

: "${GITHUB_TOKEN:?GITHUB_TOKEN must be set; the preflight authenticates the pull request head against the API}"

for tool in act gh docker; do
  command -v "${tool}" >/dev/null || {
    echo "${tool} is not on PATH." >&2
    # act and gh are both Go programs, so the toolchain this repo already needs
    # can build them. That is more reliable here than a release download,
    # because the API path to a foreign repository's releases is not always
    # reachable from a sandbox.
    [[ "${tool}" != docker ]] && echo "  install with: GOBIN=/usr/local/bin go install github.com/nektos/act@latest" >&2
    exit 1
  }
done

workdir="$(mktemp -d)"
trap 'rm -rf "${workdir}"' EXIT

# The runner image. catthehacker/ubuntu:act-latest is missing two things
# GitHub's hosted ubuntu-24.04 has and this workflow uses: ruby, which
# verify-pr-fidelity.test.sh needs, and gh, which the preflight needs. Adding
# them here rather than to the workflow keeps the workflow describing GitHub's
# runner rather than this one.
if ! docker image inspect "${IMAGE}" >/dev/null 2>&1; then
  echo "building ${IMAGE} from ${BASE_IMAGE} (adds ruby, jq, gh)"
  docker build -t "${IMAGE}" - <<CONTAINERFILE
FROM ${BASE_IMAGE}
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends ruby-full jq \
 && rm -rf /var/lib/apt/lists/*
CONTAINERFILE
fi

# A real pull_request payload, fetched rather than synthesised. act's default
# event carries no pull request number, and the preflight correctly refuses it:
# "the pull_request event carried no pull request number". Feeding it the real
# object is what makes the run verify the head this pull request actually
# proposes -- which is the whole point of GATE_SHA.
event="${workdir}/event.json"
gh api "repos/${REPOSITORY}/pulls/${pull_request}" >"${workdir}/pull-request.json"
python3 - "${workdir}/pull-request.json" "${event}" <<'PYTHON'
import json, sys

pull_request = json.load(open(sys.argv[1]))
owner, name = pull_request["base"]["repo"]["full_name"].split("/", 1)
json.dump({
    "action": "synchronize",
    "number": pull_request["number"],
    "pull_request": pull_request,
    "repository": {
        "full_name": pull_request["base"]["repo"]["full_name"],
        "name": name,
        "owner": {"login": owner},
        "default_branch": pull_request["base"]["repo"]["default_branch"],
    },
    "sender": {"login": pull_request["user"]["login"]},
}, open(sys.argv[2], "w"))
PYTHON

# Outbound HTTPS in a sandboxed session goes through an agent proxy that binds
# to loopback and re-terminates TLS. A job container reaches neither by default:
# host.docker.internal cannot see a loopback-bound listener, and the container
# does not trust the proxy's CA. --network host solves the first, mounting the
# bundle and pointing the standard CA variables at it solves the second. Nothing
# here disables verification -- that would make the run prove less than nothing.
container_options=(--network host)
environment=("${workdir}/act.env")
: >"${environment[0]}"
if [[ -n "${HTTPS_PROXY:-}" && -r /root/.ccr/ca-bundle.crt ]]; then
  container_options+=(-v /root/.ccr:/root/.ccr:ro)
  cat >"${environment[0]}" <<ENVIRONMENT
HTTPS_PROXY=${HTTPS_PROXY}
https_proxy=${HTTPS_PROXY}
SSL_CERT_FILE=/root/.ccr/ca-bundle.crt
GIT_SSL_CAINFO=/root/.ccr/ca-bundle.crt
NODE_EXTRA_CA_CERTS=/root/.ccr/ca-bundle.crt
REQUESTS_CA_BUNDLE=/root/.ccr/ca-bundle.crt
CURL_CA_BUNDLE=/root/.ccr/ca-bundle.crt
no_proxy=localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16
NO_PROXY=localhost,127.0.0.1,::1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16
ENVIRONMENT
fi
container_options+=(-v "$(command -v gh):/usr/local/bin/gh:ro")

failed=()
for job in "${jobs[@]}"; do
  log="${workdir}/act-${job}.log"
  echo "=== ${job}"
  if act pull_request -j "${job}" \
      -W "${WORKFLOW}" -e "${event}" \
      -P "ubuntu-24.04=${IMAGE}" --pull=false \
      --container-options "${container_options[*]}" \
      --container-daemon-socket /var/run/docker.sock \
      -s "GITHUB_TOKEN=${GITHUB_TOKEN}" \
      --env-file "${environment[0]}" >"${log}" 2>&1; then
    grep -E '✅  Success - Main|❌  Failure - Main' "${log}" || true
    echo "--- ${job}: ok"
  else
    grep -E '✅  Success - Main|❌  Failure - Main' "${log}" || true
    # Read the failure, do not merely report the exit code. The one step that
    # fails here for an environmental reason -- Publish pending Trunk status,
    # a commit-status write a sandbox proxy refuses -- is continue-on-error in
    # the workflow and does not fail its job. Anything else is real.
    grep -E '::error|fatal:' "${log}" | head -20 || true
    echo "--- ${job}: FAILED (full log: ${log})"
    cp "${log}" "/tmp/act-${job}.log"
    echo "    copied to /tmp/act-${job}.log"
    failed+=("${job}")
  fi
done

if [[ ${#failed[@]} -gt 0 ]]; then
  echo "failed lanes: ${failed[*]}" >&2
  exit 1
fi
echo "all requested Trunk gate lanes passed under act"
