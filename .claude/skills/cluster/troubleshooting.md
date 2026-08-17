# Troubleshooting: operational traps and scope

Sandbox-shaped problems that aren't one of the three blockers in
`reference.md`.

## Traps

### The cluster was fine, now everything is down: check for a VM restart

The sandbox VM can restart **mid-session**. It is a warm restart: the disk
survives, every process does not. Symptoms are alarming and misleading — k3s
gone, background jobs vanished, a nested VM dead, all with no error in any log.

Diagnose it in one line before assuming a crash:

```bash
uptime          # minutes, on a session that has been running for hours → restarted
```

Corroborate with a kernel thread's start time (`ps -o lstart= -p 2`) — kernel
threads cannot outlive a boot, so that is independent of the clock. (`btime`
and PID 1's start time are both *derived* from uptime, so they are not
separate evidence.)

Recovery is simply to **re-run the bootstrap** — it is idempotent, detects the
dead cluster and brings it back, and on-disk state (the k3s datastore, images,
installed packages) is still there. Do not rebuild from scratch.

**One thing does change across a restart: the agent proxy port.** Observed
45437 → 44339, while the CA bundle, hostname, `machine-id`, MAC and IP all
stayed identical. So anything that *cached* `$HTTPS_PROXY` — a written config,
a daemon started with the old environment, a value baked into an image — now
points at a dead local port and fails with connection-refused, which looks
like a proxy outage. Always read `$HTTPS_PROXY` at use time; never persist it.
Caching the CA bundle is safe. The restart cause is not observable from inside
the guest.

### `hostNetwork: true` is an anti-pattern here

Pod networking works. Host networking makes multi-replica Deployments
unschedulable on the single node, every replica competes for the same host
port, and replica 2 sits `Pending` with:

```
0/1 nodes are available: 1 node(s) didn't have free ports for the requested pod ports
```

Charts carrying `hostNetwork: true` as a workaround from an era without pod
networking should have it removed.

### Disk is a fixed allowance, so `df` misleads

"Avail" at 0 with low "Used" means the allowance is spent, not that the machine
is broken. Deletes still succeed while writes fail, and freed space is
immediately writable — **measured at exactly `f_bavail = 0`** (exp 126), not
assumed: `rm`, `truncate` and `rm -rf` all succeed on a journaled `resv_strict`
ext4 while a one-block write returns ENOSPC.

> **The one way this bites you.** Unlinking a file that any process still holds
> open frees **nothing** until the last descriptor closes. `rm` exits 0, `ls`
> shows the file gone, and `df` does not move — which reads as "deleting didn't
> help, the disk is broken" and is the wrong conclusion. Stop the writer (or
> find it: `lsof +L1`, or `ls -l /proc/*/fd 2>/dev/null | grep deleted`), then
> re-check. **Trust `df`, not `rm`'s exit code.**

**Mechanism**: the allowance is **ext4 reserved blocks**.
85 % of the filesystem is reserved for `resuid=65534` with `resv_strict`,
and root can normally dip into that pool only via `CAP_SYS_RESOURCE`, which
this sandbox withholds. So it is a hard wall for every process including
root and containers, reproducible in miniature: on a loop-mounted ext4 with
a reserve, root's `dd` stops exactly at `f_bavail` with space still
physically free. Consequences: read **`Avail` only** (`Size`/`Use%` describe
the 252 GiB device, not your budget); nothing in userspace raises it; and
cleanup is always the right response to `ENOSPC` because frees are not
capability-gated.

The kubelet's default eviction thresholds are **percentage-based** and
therefore miscalibrated for this shape. Expect warnings under image churn:

```
ImageGCFailed / FreeDiskSpaceFailed: Insufficient free disk space on the node's
image filesystem (92% of 252.0 GiB used)
```

These stayed warnings in practice, with `DiskPressure` remaining `False`. If
they ever escalate to a taint, set absolute values in the same kubelet drop-in
used for `failCgroupV1`:

```yaml
evictionHard:
  imagefs.available: "1Gi"
  nodefs.available: "1Gi"
```

### `pkill -f 'k3s server'` can kill its own shell

The pattern matches the shell's own command line. Use a bracket to break the
self-match, and start detached so a dying parent doesn't take the server with
it:

```bash
pkill -f 'k3s[ ]server'
nohup setsid k3s server ... </dev/null >>/tmp/k3s.log 2>&1 & disown
```

### Host-side NodePort needs a proxy bypass

Same rule as `reference.md` §2, applied to node-local ports:

```bash
curl --noproxy '*' http://127.0.0.1:30080
```

### The container is ephemeral

Only pushed commits survive. Clusters, images, and installed binaries do not
persist across sessions, which is why booting is scripted rather than
documented as a one-time setup.

### Changing k3s version invalidates the runc wrapper

Each release extracts a fresh `data/<hash>/bin/runc` and re-points
`data/current`. Re-run the bootstrap script after any version change; it
detects and re-installs.

## Verified capability baseline

On k3s v1.36.3+k3s1, containerd 2.3.2-k3s2, with all three fixes:

| Capability | Status |
| --- | --- |
| API server, scheduler, controller manager | works |
| kubectl CRUD, Helm install/upgrade, dry-run, RBAC | works |
| Pod execution | works |
| `kubectl exec`, `kubectl logs` | works |
| Deployments, ReplicaSets, DaemonSets | works |
| Services (ClusterIP, NodePort), in-cluster HTTP | works |
| Cluster DNS, headless-service pod DNS | works |
| PersistentVolumeClaims (local-path) | works |
| StatefulSets with `volumeClaimTemplates` | works (Postgres verified via in-pod `psql`) |
| Jobs, CronJobs, initContainers, ConfigMaps, Secrets, emptyDir | works |
| Liveness/readiness probes (exec + tcpSocket) | works |
| Plain Docker (`dockerd`, pulls, `docker exec`) | works |
| Docker Compose multi-service stacks | works |
| Podman rootful + rootless, builds, enforced limits | works (recipes in the `containers` skill) |
| k3d nested cluster | works with CA mount + `runc-oomfix-busybox` (`reference.md` §5) |
| Multi-arch (qemu/binfmt_misc), systemd containers | works (see `containers` skill, binfmt has a bricking hazard) |
| Seccomp profiles (`RuntimeDefault`), user-namespace pods (`hostUsers: false`) | work |
| NetworkPolicy enforcement, metrics-server / `kubectl top` | work |
| Cilium as CNI with kube-proxy replacement | works after patching `SYS_RESOURCE` out |
| Argo Workflows, Linkerd (sidecar injection), Knative (incl. scale-to-zero) | work |
| Istio **sidecar** mode | works out of the box: injection, STRICT mTLS, cert issuance |
| Istio **ambient** mode | works after four fixes: `IPV6_ENABLED=false` on ztunnel, `mount --make-rshared /`, copy `istio-cni` into `data/cni`, and `AMBIENT_IPV6: "false"` in `istio-cni-config` |
| Knative image resolution | needs `registries-skipping-tag-resolving`, or mount the CA: the controller resolves tags from inside a pod |
| **Rootless** k3s | **blocked**: needs a user-delegated cgroup subtree, and with no systemd and a read-only v2 hierarchy there is none — `mkdir` in `/sys/fs/cgroup/unified` is denied even to root. Rootful is unaffected |

## Out of scope or impossible

- **Multi-node**: out of scope for this plugin, not impossible. A qemu TCG
  guest joins as a `Ready` second node and, with a tap netdev, carries a
  *verified* cross-node dataplane — pod↔pod, ClusterIP with a remote endpoint,
  DNS and egress. It needs an 8-fix custom guest image,
  ~8 GB guest RAM and a ~17-18× emulation penalty, so the plugin's bootstrap
  stays single-node.
- **Production-representative benchmarking**: microVM on shared host.
- **Anything depending on cgroup v2**: this sandbox is v1, and the opt-out for
  Kubernetes ≥1.35 has a known expiry.
- **Hard kernel limits** (definitive on *this* kernel, do not retry here):
  CRIU checkpoint/restore (`CONFIG_CHECKPOINT_RESTORE` absent, `kcmp` ENOSYS),
  IPv6 (`ipv6.disable=1`, and the module param is read-only even to root),
  kernel modules, nested virtualization (`/dev/kvm`), and reacquiring
  `cap_sys_resource` (`reference.md` §6). Full table in the `containers`
  skill. All except `/dev/kvm` are properties of this kernel image rather
  than absolutes — a TCG guest kernel has IPv6, checkpoint/restore, cgroup
  delegation and the full capability set.

## When this document is wrong

This environment was swapped once already (gVisor/9p → Firecracker/ext4),
which invalidated an entire body of prior research including its stated root
cause. If what you observe contradicts this text, the text is the more likely
thing to be stale. Re-measure the premises first, kernel, filesystem, cgroup
version, proxy behavior, before building a workaround on top of a claim that
may no longer hold.
