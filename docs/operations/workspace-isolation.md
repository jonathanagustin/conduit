# Workspace isolation status and admission

The local `process` adapter starts a host process with a filtered environment.
A working directory does not confine filesystem access, and its `DialContext`
helper only checks calls made through that helper. A child can open sockets or
read absolute host paths directly. Those mechanisms do not establish container
isolation, a rootless builder, absence of filesystem credentials or enforced
egress policy.

`NewWithEgress` validates configuration and supports cooperative dial checks,
but `Start` refuses to execute a host process when that policy is requested.
`ExecInWorkspace` also refuses execution because no isolated executor is wired.
Both return the existing unavailable-workspace error; no child is started.
Ordinary local previews retain their explicitly uncontained process behavior.
The UI states that isolation must be configured and local previews use the host.

## Rendered configuration versus runtime enforcement

The `workspace.egress` YAML parser validates a required operator-supplied Git
host and optional additional entries. The registry defaults to
`registry.uds.svc.cluster.local:5000`. Wildcards, allow-all CIDRs and empty entries
are rejected. `RenderPod` requires this configuration explicitly; it does not
invent a Git host when none was supplied.

The rendered Pod requests a non-root user, read-only root filesystem, dropped
capabilities, disabled privilege escalation, no service-account token and no
service links. Builder environment variables are requests, not proof that an
admitted rootless builder exists in the selected image. Credential-variable
filtering and a scan of a controlled directory do not prove that an arbitrary
host process cannot read credentials elsewhere.

Standard Kubernetes NetworkPolicy does not match DNS names. The current draft
renderer represents name-based peers with labels, which does not establish
connectivity to or enforcement for the configured DNS host. The cluster adapter
therefore refuses provisioning without explicit operator egress configuration
or when entries require unsupported DNS enforcement. Explicit IP/CIDR entries
can be rendered; the network policy must apply successfully before the Pod is
created. A missing cluster cannot establish a real workload or its isolation.
The generated Package CR alone does not prove it was installed or enforced.

## Remaining qualification

Issue #616 remains open for the operator installation-config loader, admitted
workspace image and rootless builder, DNS-aware policy backend, and live tests
inside the workload proving allowed endpoints, blocked endpoints, protected
policy and absent credentials. Once observed for that runtime, its posture is
“container isolation now, stronger isolation planned.” Rendering manifests or
testing a cooperative Go dial method does not close those requirements.

Regression coverage establishes raw socket and out-of-workspace file access in
an ordinary child as a positive control, then verifies that isolation requests
are refused before that child runs. Cluster adapter tests verify no apply for
missing/unsupported configuration and policy-before-Pod ordering, including a
failed policy apply. Those tests do not deploy or qualify a Kubernetes cluster.
