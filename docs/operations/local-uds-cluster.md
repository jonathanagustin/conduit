# Local UDS cluster selection

`mise run uds-up` and `mise run uds-down` manage one local k3d cluster. They use
the working engine selected by `scripts/lib/development-host.sh`: an explicit
`CONDUIT_CONTAINER_ENGINE` executable, or Docker followed by Podman. An explicit
unavailable engine fails instead of falling back. The default Docker/Linux
configuration remains cluster `uds`, network `uds-k3d`, Core slim-dev `1.17.1`
and the single-node flannel `host-gw` arguments.

## Docker API endpoint on macOS

The selected engine CLI manages the network, while UDS/k3d also need its
Docker-compatible API. A working `podman info` alone does not establish that
API connection. For Podman, set `DOCKER_HOST` to the endpoint for the **same**
running machine/connection as the selected CLI. For example, after selecting
the intended machine:

```sh
export CONDUIT_CONTAINER_ENGINE=/opt/podman/bin/podman
podman_socket="$("${CONDUIT_CONTAINER_ENGINE}" machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}' podman-machine-default)"
export DOCKER_HOST="unix://${podman_socket}"
```

Use the actual machine name and executable location on the host. Podman's
[macOS endpoint instructions](https://podman-desktop.io/docs/migrating-from-docker/using-the-docker_host-environment-variable)
describe this inspection. The scripts do not start a machine, change its
connection, discover credentials or change socket permissions. They require an
explicit `DOCKER_HOST` for Podman or another non-Docker executable. A
`DOCKER_CONTEXT` without `DOCKER_HOST` is rejected because it selects a Docker
CLI context without identifying the API endpoint to UDS/k3d. With an explicit
`DOCKER_HOST`, the scripts clear `DOCKER_CONTEXT` in their own process so that
Docker CLI operations use that endpoint, too.

k3d describes Podman support as experimental. Its
[Podman guide](https://k3d.io/v5.6.2/usage/advanced/podman/) covers API service,
socket mount and rootless engine prerequisites, including `DOCKER_SOCK` where
needed. These scripts pass the configured environment through; they do not
configure the VM or establish that every Core component works on macOS.

## Cluster, network, ports and kubeconfig

Use a unique cluster name for concurrent tasks. The default network follows
that name, and the task kubeconfig defaults to the absolute repository path
`.cache/uds/<cluster>.kubeconfig`. `UDS_KUBECONFIG` can select one other absolute,
task-owned file; a colon-separated kubeconfig list is refused. The scripts
replace inherited `KUBECONFIG` with that file before UDS/k3d operations.

```sh
export UDS_K3D_CLUSTER=conduit-local-922
export UDS_K3D_NETWORK=conduit-local-922-k3d
export UDS_KUBECONFIG="${PWD}/.cache/uds/conduit-local-922.kubeconfig"
export UDS_K3D_API_PORT=16550
export UDS_K3D_HTTP_PORT=18080
export UDS_K3D_HTTPS_PORT=18443
./scripts/uds-up.sh --print-plan
./scripts/uds-up.sh --dry-run
mise run uds-up
```

`--print-plan` needs no engine and runs no cluster commands. `--dry-run` checks
the selected engine with `info`, then prints the proposed commands. Neither
creates a cluster or applies the host adaptations.

Choose unused host ports for concurrent tasks. The optional
`UDS_K3D_API_PORT`, `UDS_K3D_HTTP_PORT` and `UDS_K3D_HTTPS_PORT` settings forward
to the bundle's `K3D_API_PORT`, `K3D_HTTP_PORT` and `K3D_HTTPS_PORT` variables.
Each must be from 1 to 65535. When unset, the bundle defaults remain 6550, 80
and 443. A unique cluster name does not make those defaults unique.

`K3D_EXTRA_ARGS` replaces the whole default argument string. When adding other
port mappings, retain the selected `--network` and required flannel argument.
The script forwards this string as one bundle-variable argument rather than
evaluating it as shell code.

The deploy explicitly sets `CLUSTER_NAME=<UDS_K3D_CLUSTER>` and
`K3D_EXTRA_ARGS=...`. Read-only inspection on 2026-11-17 with UDS CLI `0.41.0`
of the pinned Core `1.17.1` **arm64** slim-dev bundle found those variables in
`uds-k3d-dev`, with `CLUSTER_NAME` defaulting to `uds`. Its pinned child is
`uds-k3d` `0.21.4-airgap` at
`sha256:38028585aaf4df616ad9f0f1af6d94becf614850f564a2bd59de522c55f9f824`.
This is bundle metadata evidence, not a Core deployment, Conduit installation
or metrics scrape.

An existing task kubeconfig must select `k3d-<cluster>` before deployment.
After deployment, `k3d kubeconfig get <cluster>` writes a private temporary
file. Both its selected context and named context must be verified successfully
before it replaces the task file. Every API read and host adaptation supplies
both `--kubeconfig <task file>` and `--context k3d-<cluster>`, including the
bundled `uds zarf tools kubectl` fallback. A failed export, context check or API
readiness wait stops before the nginx/Istio adaptations. No ambient context is
merged or selected. k3d's
[kubeconfig get command](https://k3d.io/v5.8.3/usage/commands/k3d_kubeconfig_get/)
documents the explicit cluster export.

Use the same selection variables for `mise run uds-down`. It requires k3d and
successful named-cluster deletion before network cleanup. It does not fall
back to deleting a kubectl context when k3d is unavailable, and it leaves an
existing network while another container is attached. It does not remove the
task directory or independently delete kubeconfig contexts.

## Offline regression checks

Run `./scripts/test-uds-up.sh` and `./scripts/test-development-host.sh`. The
lifecycle fixture uses a curated command path with fake engines, UDS, k3d and
kubectl, checks adaptation payloads and refusal paths, and verifies an ambient
kubeconfig sentinel remains unchanged. It contacts no real cluster. These
checks establish command selection and isolation; a real pinned Core deployment
and application verification remain separate evidence.
