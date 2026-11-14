#!/usr/bin/env bash
# Registry and image-build networking for scripts/demo-seed.sh.
# The default is bridge publishing. Host networking is used when
# DEMO_DOCKER_NETWORK=host, or when bridge publishing or the image build
# fails because the daemon has no iptables or bridge network.

docker_network_failure() {
	local text="$1"
	local pattern='iptables|ip6tables|docker0|network sandbox|failed to create endpoint|No chain/target/match|bridge|set up container networking|Failed to Setup IP tables|is not a working network|failed to create network'
	[[ "${text}" =~ ${pattern} ]]
}

demo_prepare_docker_network() {
	docker_network="${DEMO_DOCKER_NETWORK:-bridge}"
	case "${docker_network}" in
	bridge | host) ;;
	*)
		echo "DEMO_DOCKER_NETWORK must be bridge or host (got ${docker_network})." >&2
		return 1
		;;
	esac
}

demo_start_registry() {
	local err="" image="$1"
	docker rm -f conduit-seed-registry >/dev/null 2>&1 || true
	if [[ "${docker_network}" == "host" ]]; then
		docker run --detach --name conduit-seed-registry --network host \
			-e REGISTRY_HTTP_ADDR=127.0.0.1:5000 \
			"${image}" >/dev/null
		return 0
	fi
	if err="$(docker run --detach --name conduit-seed-registry --publish 127.0.0.1:5000:5000 "${image}" 2>&1)"; then
		return 0
	fi
	docker rm -f conduit-seed-registry >/dev/null 2>&1 || true
	if docker_network_failure "${err}"; then
		echo "Docker could not publish 127.0.0.1:5000 with bridge networking; starting the registry on the host network." >&2
		docker_network="host"
		docker run --detach --name conduit-seed-registry --network host \
			-e REGISTRY_HTTP_ADDR=127.0.0.1:5000 \
			"${image}" >/dev/null
		return 0
	fi
	printf '%s\n' "${err}" >&2
	return 1
}

demo_docker_build() {
	local tag="$1" dir="$2" err=""
	if [[ "${docker_network}" == "host" ]]; then
		docker build --network host -t "${tag}" "${dir}" >&2
		return 0
	fi
	if err="$(docker build -t "${tag}" "${dir}" 2>&1)"; then
		if [[ -n "${err}" ]]; then
			printf '%s\n' "${err}" >&2
		fi
		return 0
	fi
	if docker_network_failure "${err}"; then
		echo "Docker build could not use bridge networking; retrying with --network host." >&2
		docker_network="host"
		docker build --network host -t "${tag}" "${dir}" >&2
		return 0
	fi
	printf '%s\n' "${err}" >&2
	return 1
}
