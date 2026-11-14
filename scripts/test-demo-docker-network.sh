#!/usr/bin/env bash
# The demo registry keeps bridge publishing when it works, and uses host
# networking only for DEMO_DOCKER_NETWORK=host or an iptables/bridge failure.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=SCRIPTDIR/lib/demo-docker-network.sh
source "${root}/scripts/lib/demo-docker-network.sh"

fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
log="${fixture}/docker.log"
cat >"${fixture}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${DOCKER_FAKE_LOG:?}"
mode="${DOCKER_FAKE_MODE:-publish-ok}"
if [[ "${1}" == "rm" ]]; then
	exit 0
fi
if [[ "${1}" == "run" ]]; then
	case "${mode}" in
	publish-ok | build-ok)
		exit 0
		;;
	publish-fails | build-fails)
		if [[ " $* " == *" --publish "* ]]; then
			echo "driver failed programming external connectivity: iptables: No chain/target/match by that name" >&2
			exit 1
		fi
		if [[ " $* " == *" --network host "* ]]; then
			exit 0
		fi
		echo "unexpected docker run: $*" >&2
		exit 1
		;;
	daemon-down)
		echo "Cannot connect to the Docker daemon at unix:///var/run/docker.sock." >&2
		exit 1
		;;
	*)
		echo "unknown fake mode ${mode}" >&2
		exit 1
		;;
	esac
fi
if [[ "${1}" == "build" ]]; then
	case "${mode}" in
	publish-ok | build-ok)
		exit 0
		;;
	build-fails)
		if [[ " $* " == *" --network host "* ]]; then
			exit 0
		fi
		echo "failed to create network: network bridge not found" >&2
		exit 1
		;;
	*)
		echo "unexpected docker build in mode ${mode}: $*" >&2
		exit 1
		;;
	esac
fi
exit 0
EOF
chmod 0755 "${fixture}/docker"
export PATH="${fixture}:${PATH}"
export DOCKER_FAKE_LOG="${log}"

assert_contains() {
	local want="$1"
	if ! grep -F -q -- "${want}" "${log}"; then
		echo "docker log missing: ${want}" >&2
		cat "${log}" >&2
		exit 1
	fi
}

assert_excludes() {
	local want="$1"
	if grep -F -q -- "${want}" "${log}"; then
		echo "docker log unexpectedly contained: ${want}" >&2
		cat "${log}" >&2
		exit 1
	fi
}

reset_case() {
	unset DEMO_DOCKER_NETWORK || true
	docker_network="bridge"
	: >"${log}"
}

if ! docker_network_failure "iptables: No chain/target/match by that name"; then
	echo "iptables failure was not recognized" >&2
	exit 1
fi
if ! docker_network_failure "failed to create network: network bridge not found"; then
	echo "bridge failure was not recognized" >&2
	exit 1
fi
if docker_network_failure "Cannot connect to the Docker daemon at unix:///var/run/docker.sock."; then
	echo "a stopped daemon was treated as a bridge failure" >&2
	exit 1
fi
if docker_network_failure "manifest unknown"; then
	echo "an image error was treated as a bridge failure" >&2
	exit 1
fi

reset_case
export DOCKER_FAKE_MODE=publish-ok
demo_prepare_docker_network
demo_start_registry "registry@sha256:abc"
assert_contains "--publish"
assert_excludes "--network"
assert_excludes "REGISTRY_HTTP_ADDR"
[[ "${docker_network}" == "bridge" ]]

reset_case
export DOCKER_FAKE_MODE=publish-fails
demo_prepare_docker_network
demo_start_registry "registry@sha256:abc"
assert_contains "--publish"
assert_contains "--network host"
assert_contains "REGISTRY_HTTP_ADDR=127.0.0.1:5000"
[[ "${docker_network}" == "host" ]]

reset_case
export DOCKER_FAKE_MODE=publish-ok
DEMO_DOCKER_NETWORK=host
demo_prepare_docker_network
demo_start_registry "registry@sha256:abc"
assert_contains "--network host"
assert_contains "REGISTRY_HTTP_ADDR=127.0.0.1:5000"
assert_excludes "--publish"
[[ "${docker_network}" == "host" ]]

reset_case
export DOCKER_FAKE_MODE=daemon-down
demo_prepare_docker_network
if demo_start_registry "registry@sha256:abc"; then
	echo "a stopped daemon fell through instead of failing" >&2
	exit 1
fi
assert_contains "--publish"
assert_excludes "--network host"

reset_case
export DOCKER_FAKE_MODE=build-ok
demo_prepare_docker_network
demo_docker_build "127.0.0.1:5000/sample-app:0.1.0" "${fixture}"
assert_excludes "--network"
[[ "${docker_network}" == "bridge" ]]

reset_case
export DOCKER_FAKE_MODE=build-fails
demo_prepare_docker_network
demo_docker_build "127.0.0.1:5000/sample-app:0.1.0" "${fixture}"
assert_contains "--network host"
[[ "${docker_network}" == "host" ]]

reset_case
if DEMO_DOCKER_NETWORK=overlay demo_prepare_docker_network; then
	echo "an unknown DEMO_DOCKER_NETWORK value was accepted" >&2
	exit 1
fi

echo "demo docker network fallback ok"
