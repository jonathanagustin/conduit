# Workspace isolation

Governed workspace commands run in a container on a dedicated internal network.
The posture is: container isolation now, stronger isolation planned.

| Claim | Class | Basis | Not established |
| --- | --- | --- | --- |
| Default-deny egress except the operator git host and local registry | Conduit extension | `workspace.egress` validation and the live container probe | A Kubernetes DNS network-policy backend. The cluster adapter still refuses name-based rules. |
| No cluster credentials in the workspace container | Conduit extension | Launch contract and the probe's view of mounts, environment, and credential paths | A scan of every host secret outside the container |
| Non-root execution without the host container socket | Conduit extension | Observed uid 65534, dropped capabilities, read-only root, empty mounts | An admitted image build or a rootless builder producing an image. That remains #615. |
| Container isolation now, stronger isolation planned | Conduit extension | This runtime | A separate machine or VM boundary |

`DEVWORKSPACE_RUNTIME=container` selects the runtime. `DEVWORKSPACE_GIT_HOST` is required. The registry defaults to `registry.uds.svc.cluster.local:5000`. Wildcard hosts, `0.0.0.0/0`, and an empty git host fail validation and do not start a process. The workspace container cannot change that allowlist: it has no socket, no credential, and no capability to write a policy file. An operator change goes through the existing change-request review.

The container joins only the internal network. A forwarder is the only peer, and it dials the configured git host and registry ports. Connections to any other name, address, or port do not reach that forwarder. A policy listener on the forwarder's other network answers there and is unreachable from the workspace. The host can read a planted kubeconfig; the container cannot, and the host `KUBECONFIG` and `SERVICEACCOUNT_TOKEN` variables are not copied in.

`POST /v1/planning-sessions/{id}/compute` returns `COMPUTE_NOT_CONFIGURED` until `CONDUIT_IMAGE_BUILDER=docker` admits the local engine. That build runs on the host engine because this container has no builder and no container socket. Selecting the container runtime makes `plan-compute` refuse to continue unless the live probe passes. It does not turn the fixture package writer into an image build.

A local process preview is a host process. Its working directory and filtered environment do not confine sockets or host files. The console states that limit next to the container posture. Rendering a Pod, or checking a dial made through the process adapter, is not this boundary.

Cgroup quotas are not part of this claim. Stronger isolation than a container is still planned.
