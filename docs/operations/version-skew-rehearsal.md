# Agent and Hub source-version rehearsal

`./scripts/test-version-skew.sh /tmp/version-skew-receipt.json` builds production
Hub and Spoke executables from two independently archived source revisions:
current HEAD and the newest stable `vX.Y.Z` tag at a different commit. Fetch
tags first. Both builds use the current Go compiler; this tests source versions,
not byte identity with previously published executables or their complete
release container images. Build metadata and the receipt distinguish commits
even while both versions still report the same semantic version.

The manual **Agent Hub version skew** workflow runs the same test with Go from
`go.mod`, checksum-pinned Helm 4.2.3 and kubectl 1.31.5, read-only repository
permission, cancellation of superseded runs and a twenty-minute limit. Local
runs require those tools, Python 3, Git, tar, OpenSSL and Docker on PATH. The
receipt records actual tool versions and hashes, source commits, all four
production binary hashes, database/registry/Kubernetes image digests and the
registry-reported chart digest. No product image is published.

The fixture starts a real isolated Kubernetes API using K3s's supported
`--disable-agent` server mode with no added container capabilities. There is no
kubelet, node or Pod scheduler. The real chart contains only a ConfigMap, which
lets Helm exercise OCI pull, digest verification, release storage, install and
post-render metadata against a real TLS-verified API under rootless Docker.
This is not evidence for workload scheduling, node compatibility or RBAC of a
production Spoke deployment. No existing kubecontext is selected or modified.

Two cases execute sequentially: previous Agent/current Hub, then current
Agent/previous Hub. Each has its own PostgreSQL database, environment namespace,
temporary directory, Helm configuration and registry credentials file. The Hub
and Agent use real production mTLS identity checks with generated fixture
certificates and a mounted signing key. Product runtime environment variables,
external material commands, personal Helm plugins and OIDC/provider settings
are not inherited. The local OCI registry, database and Kubernetes listeners
bind only loopback.

The operator creates an ordinary deployment Plan through the Hub API under an
explicit reviewer-team policy. The Agent polls while the Plan awaits approval;
the test asserts it remains unexecuted. A separate reviewer identity then
approves it. The real Spoke binary must pull the pinned chart, install it with
real Helm, stamp metadata and finish the Plan successfully. Assertions read the
actual ConfigMap value and annotations, then require the exact environment,
Agent, Plan, installation, Product, version, namespace, release digest and
configuration digest in the Hub report, with terminal lifecycle/config/rollout
states and real observation timestamps. A successful Plan alone cannot pass.
The fixture image reference is ConfigMap data; no workload image is pulled.

The runner terminates only its processes, removes its containers, and deletes
fixture keys, kubeconfig and temporary sources on exit. The receipt is written
after both directions succeed and contains no key material or database DSNs.

This addresses the wire-version portion of #263 for this pinned chart/tool
fixture. [The old-data rehearsal](old-data-upgrade-rehearsal.md) separately
checks populated data preservation. The separate
[live image replacement rehearsal](hub-rolling-upgrade-rehearsal.md) rolls two
serving Hub images with work in flight and checks renewed leases, atomic
decisions and preserved proposals. Its driver-managed scope does not establish
Kubernetes Deployment-controller behavior or production capacity.
