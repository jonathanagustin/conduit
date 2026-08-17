---
name: cluster
description: Boot and troubleshoot a full Kubernetes (k3s) cluster inside a Claude Code on the web sandbox, where pods, kubectl logs, and modern k3s all fail by default. Also covers running Docker, Postgres, or testcontainers in a web session.
when_to_use: Use when the user wants a Kubernetes cluster, k3s, or kubectl in a web session; when a pod is stuck in ContainerCreating with a runc error "can't get final child's PID from pipe: EOF"; when kubectl logs returns EOF; when k3s or kubelet exits citing cgroup v1; when images will not pull inside a cluster; when a test suite needs Docker, testcontainers, or a real Postgres in a web session; when the user wants a service mesh or Kubernetes ecosystem tooling in the cluster (Istio sidecar or ambient, Linkerd, Cilium as CNI, Argo Workflows, Tekton) — all verified working here with the fixes noted in this skill; or when a tool hardcodes k3d or kind.
allowed-tools: Bash(${CLAUDE_SKILL_DIR}/scripts/bootstrap-k3s.sh *) Bash(${CLAUDE_SKILL_DIR}/scripts/import-image.sh *)
---

# Kubernetes inside a Claude Code web sandbox

A full k3s cluster runs here, pods, `exec`, `logs`, Deployments, Services,
DNS, PVCs, StatefulSets, Jobs, CronJobs, NodePort, but three sandbox
properties break it by default. Each fails in a way that looks like broken
code rather than a broken environment, so **diagnose from the table below
rather than guessing**; every symptom has one specific known cause.

This skill is self-contained: its scripts sit in `scripts/` beside this file.

## Boot a cluster

Check whether one is already running:

```bash
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl get nodes && kubectl get pods -A
```

If not, boot it. Idempotent, a healthy cluster short-circuits in one probe:

```bash
sudo ${CLAUDE_SKILL_DIR}/scripts/bootstrap-k3s.sh
```

The script applies all three fixes in the order that matters (k3s only extracts
its runc on first run of a version, so the wrapper is installed *after* start
and *before* the readiness gate). Log: `/tmp/k3s-bootstrap.log`.

Then verify it genuinely works, run a pod and read its logs back, since `logs`
is the capability most likely to be silently broken:

```bash
kubectl run check --image=busybox:1.37 --restart=Never -- echo ok
sleep 15 && kubectl logs check && kubectl delete pod check --now
```

## Diagnose

| Symptom | Cause | Fix |
| --- | --- | --- |
| Pods stuck `ContainerCreating`; runc `can't get final child's PID from pipe: EOF` | Kernel rejects negative `oom_score_adj`; CRI pins `-998` | Install the runc wrapper → `reference.md` §1 |
| `kubectl logs` returns `EOF` | Egress proxy eats apiserver→kubelet dials | Extend `NO_PROXY` → `reference.md` §2 |
| Admission webhook fails: `failed calling webhook` / timeout | Same cause as the `logs` row — the apiserver's callback to a ClusterIP is being proxied | Same fix: the pod and Service CIDRs must be in the **server's** `NO_PROXY`. With the bootstrap applied webhooks work (admit *and* deny verified), so operators, cert-manager, sidecar injectors and policy controllers are all usable |
| k3s/kubelet exits citing cgroup v1 | Kubernetes ≥1.35 refuses cgroup v1 | `failCgroupV1: false` drop-in → `reference.md` §3 |
| Images won't pull in-cluster | k3s didn't inherit `HTTPS_PROXY` | `scripts/import-image.sh` → `reference.md` §4 |
| Any TLS failure | Proxy re-terminates TLS | Point the tool at the CA bundle; never disable verification |
| A process *inside a container* gets `connection refused` to `127.0.0.1:<port>` | Host proxy env forwarded into a foreign netns | `--http-proxy=false` (podman) / strip proxy env → `containers` skill |
| `hostNetwork` pods unschedulable; disk/ImageGC warnings; `pkill` killing its own shell | Sandbox-shaped traps | `troubleshooting.md` |
| CRIU, IPv6, kernel modules, or nested virt requested | Hard kernel limits for *this* kernel | `containers` skill hard-limit table. A nested qemu TCG VM escapes them all at ~10x slowdown |
| `crun: cgroups in hybrid mode not supported` | crun installed and became podman's default | `--runtime runc`, or reset `containers.conf` |
| `path ... is mounted on "/" but it is not a shared mount` | The root mount is `private` here | `mount --make-rshared /`, then restart the pod |
| KubeVirt `virt-handler` panics on `failed to detect the presence of selinux` | Not SELinux: virt-handler runs `getenforce` via `virt-chroot --mount /proc/1/ns/mnt`, and with `hostPID:true` PID 1 is the host `process_api` whose mount ns is unreadable (T12), so the exec fails (exp 131) | **Control plane runs after one env var**: `kubectl set env deploy/virt-operator -n kubevirt KV_IO_EXTRA_ENV_VIRT_IN_CONTAINER=true` (switches detection to the PPID namespace), plus `mount --make-rshared /`. KubeVirt then reaches `Deployed`. **A VM boots** (exp 131, CirrOS guest to login under TCG) with a runtime recipe: keep virt-handler `hostPID:true` but wrap its entrypoint with a `/proc/1` bind-shim (`mount --bind /proc/$PPID /proc/1`, the containerd-shim = a readable node stand-in, so KubeVirt's hardcoded `/proc/1/{root,ns,mountinfo}` resolve while the host PID ns is kept for launcher `SO_PEERCRED`); run k3s/containerd as root with `ulimit -c unlimited` (else containers inherit hard RLIMIT_CORE=0 and libvirt can't raise it); use `masquerade` + explicit `ports:` for full networking (only `bridge` binding fails — it needs the unsupported `dummy` netdev). Exp 132 hardens/minimizes this (operator stays up via `customizeComponents`, ancestor-walk shim, core-dump guard) and packages it as the opt-in **`kubevirt` skill module** (install/verify/uninstall, verified end-to-end) — but for just a running guest, the qemu-TCG escape hatch is 1–2 orders simpler; KubeVirt earns it only for k8s-native VM ops. See exps 131–132 |
| `nsenter: cannot open /hostproc/1/ns/...: Permission denied` | PID 1 is the harness control process and its namespaces are shielded from everyone, including root | Do the work on the host instead and disable the agent's init container |
| `unable to apply caps: can't apply capabilities: operation not permitted` | The pod's securityContext requests `SYS_RESOURCE`, which the launcher withholds | Patch that capability out. Cilium needed it only for a pre-5.11 BPF rlimit and works without it |
| A controller reports `x509: certificate signed by unknown authority` while the **host** pulls the same image fine | The component does registry work *from inside a pod*, and pods do not inherit the session CA | Mount `/root/.ccr/ca-bundle.crt`, or skip the step. Knative needs `registries-skipping-tag-resolving` |
| `Address family not supported by protocol (os error 97)` at startup | The process binds a dual-stack socket, and IPv6 is disabled by the `ipv6.disable=1` boot parameter | Use the product's IPv4/IPv6 switch. For Istio ambient: `IPV6_ENABLED=false` on the ztunnel DaemonSet plus `AMBIENT_IPV6: "false"` in `istio-cni-config`; ambient then works fully |

**Most third-party failures here are one of the rows above.** Match the
symptom before investigating: across real ecosystem software, every failure
traced back to a property already listed rather than
to something new.

`scripts/bootstrap-k3s.sh` already handles all three blockers. Read
`reference.md` when the script isn't an option, when you want to confirm a
diagnosis before acting, or when behavior doesn't match this table.

## Images

A k3s started with the session's proxy environment pulls directly, usually
nothing to do. Otherwise stage from host Docker:

```bash
sudo ${CLAUDE_SKILL_DIR}/scripts/import-image.sh myimage:tag
```

Images **must** land in containerd's `k8s.io` namespace, the one the kubelet's
CRI reads; an import elsewhere reports success and is invisible to pods. The
script verifies this rather than trusting an exit code.

## Docker alone (no Kubernetes)

Plain Docker needs no special handling beyond starting the daemon:

```bash
sudo dockerd >/tmp/dockerd.log 2>&1 &
```

That is enough for testcontainers-style suites and real Postgres integration
gates. The `oom_score_adj` problem never arises because ordinary containers
default to `0`, which is also why Docker works here while Kubernetes pods do
not.

For everything beyond plain docker, podman (rootful/rootless), the podman
docker-API socket, compose, `kube play`, multi-arch/arm64, systemd containers,
nested containers, and the sandbox's hard kernel limits, see the sibling
**`containers` skill**.

## Prefer native k3s; k3d and kind both work if a tool demands it

Native k3s is the zero-moving-parts path. Both nested provisioners work too,
each hitting the same `oom_score_adj` wall and fixed the same way:

- **kind**: create with `--retain`, install `scripts/runc-oomfix` over
  `/usr/local/sbin/runc` in the node (bash and python3 are present, so the
  standard wrapper runs), re-run `kubeadm init`, then apply the CNI by hand.
- **k3d**: mount the session CA at cluster create, and install
  `scripts/runc-oomfix-busybox` (node has busybox sh only). `k3d image
  import` silently lands nothing; use `docker save | ctr -n k8s.io images
  import -`.

Full recipes in `reference.md` §5 and §7. **Never install `crun`**: it
refuses cgroup v1 hybrid mode and silently becomes podman's default,
breaking a working podman.

## Files

| File | Contents |
| --- | --- |
| `reference.md` | Each blocker: cause, how to confirm it, the fix, and why the obvious alternative fails |
| `troubleshooting.md` | Operational traps, verified capability baseline, what's out of scope |
| `scripts/bootstrap-k3s.sh` | Boots a cluster with all fixes applied |
| `scripts/runc-oomfix` | The runc wrapper used by blocker 1 |
| `scripts/runc-oomfix-busybox` | Same clamp for k3d nodes (busybox sh only) |
| `scripts/import-image.sh` | Host Docker → cluster containerd |
| `../containers/SKILL.md` | Podman/docker fidelity recipes and hard kernel limits |

> **Sandbox facts have a shelf life.** This environment was swapped once
> already (gVisor/9p → Firecracker/ext4), invalidating a large body of prior
> research. If observed behavior contradicts this skill, re-measure and treat
> the text as stale rather than the observation as wrong.
