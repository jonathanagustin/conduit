# Reference: the sandbox blockers in detail

Read this when `scripts/bootstrap-k3s.sh` isn't an option, when you want to
confirm a diagnosis before changing anything, or when observed behavior
contradicts the skill.

Each section is: symptom → cause → how to confirm → fix → why the obvious
alternative fails.

## The sandbox

| Property | Value |
| --- | --- |
| Virtualization | Firecracker microVM, kernel `6.18.5-fc-v20` |
| Root filesystem | ext4 on `/dev/vda` |
| cgroups | **v1** |
| Privilege | root; near-full capabilities, `cap_sys_resource` is absent from PID 1's bounding set and not reacquirable by any descendant — though a kernel-spawned `usermodehelper` child does hold it (§6) |
| Primary IP | typically `192.0.2.2` (RFC 5737 TEST-NET-1) |
| Egress | all HTTPS via proxy on `127.0.0.1` (**port varies per session**: read `$HTTPS_PROXY`), TLS re-terminated, CA at `/root/.ccr/ca-bundle.crt`; egress is transparently intercepted even with the proxy env unset |
| Lifetime | ephemeral; only pushed commits survive |
| Disk | fixed per-session allowance |

Two of these cause nearly every difficulty: **cgroup v1** and the **egress
proxy**. The rest is an ordinary Linux box, which is why so much works once
those are handled.

---

## §1: Kernel rejects negative `oom_score_adj`

**Symptom.** Every pod sandbox dies identically:

```
failed to create shim task: OCI runtime create failed: runc create failed:
unable to start container process: can't get final child's PID from pipe: EOF
```

**Cause.** Any write below a process's `oom_score_adj_min` floor — a
`task_struct` field with **no procfs entry**, so it cannot be read; its value
is inferred from the inheritance rule, which is source-verified in exp 83 (0 for
everything here, since PID 1 starts at 0) fails with **EACCES**: this is
**mainline Linux semantics, not a patched kernel**: going
below the floor requires `CAP_SYS_RESOURCE`, which is absent from this
sandbox's bounding set and not recoverable by descendants (§6). Note: shell `echo` reports
the failure as "I/O error", which earlier docs mistook for EIO, the real
errno is 13/EACCES; diagnose with strace or a raw `write(2)`.

**Do not treat the legacy `/proc/<pid>/oom_adj` as a synonym.** It returns the
same EACCES here, but for a different reason: upstream compares `oom_score_adj`
against the *floor* and `oom_adj` against the *current value*. Those coincide
only while floor == current, which is this sandbox's resting state. Raise
`oom_score_adj` to 500 and writing `0` succeeds via the modern interface and
fails EACCES via the legacy one (exp 83, predicted from source then confirmed).
Legacy also rescales its input and never updates the floor. The kubelet
and the CRI hardcode `oomScoreAdj: -998` on every pod sandbox, so runc's child
dies during setup before any pod can start.

**Confirm** (both should fail):

```bash
sh -c 'echo -998 > /proc/self/oom_score_adj'              # I/O error
docker run --rm --oom-score-adj=-998 busybox:1.37 true    # same EOF
```

For the full picture, a one-flag bisect isolates it, `--ipc=shareable`,
`--network=none`, and the pause image itself all succeed; only
`--oom-score-adj=-998` reproduces. This also explains the otherwise-baffling
asymmetry that plain `docker run` always works while pods never do: ordinary
containers default to `0`.

**Not a runc bug.** Given an identical bundle, system runc 1.3.4 and k3s's
embedded 1.2.6 both succeed at `0` and both fail at `-998`.

**Fix.** Shadow the runc containerd resolves with `scripts/runc-oomfix`, which
clamps negative `oomScoreAdj` to `0` in the OCI spec (and in `runc exec`
process documents), then execs the real binary. Inside a single-tenant microVM
the OOM-protection intent of `-998` is moot, nothing else competes to be
killed.

```bash
D=/var/lib/rancher/k3s/data/current/bin
cp -a "$D/runc" "$D/runc.real"
install -m0755 scripts/runc-oomfix "$D/runc"
sed -i "s#^REAL_RUNC=.*#REAL_RUNC=\"$D/runc.real\"#" "$D/runc"
```

**Why not a containerd config template.** Setting `BinaryName` in
`config-v3.toml.tmpl` is the documented route, but k3s's base template already
emits a
`[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.runc.options]`
table. A second one makes containerd exit immediately with
`failed to unmarshal TOML: toml: table options already exists`. Shadowing the
binary sidesteps this.

> **Re-install after every k3s version change.** Each release extracts a fresh
> `/var/lib/rancher/k3s/data/<hash>/bin/runc` and re-points `data/current`.

---

## §2: The egress proxy eats apiserver→kubelet traffic

**Symptom.** Everything works except `kubectl logs`:

```
Error from server: Get "https://<node-ip>:10250/containerLogs/...": EOF
```

**Cause.** The session routes all HTTPS through an egress proxy
(`HTTPS_PROXY=http://127.0.0.1:<session-port>`). Its default `no_proxy` covers loopback,
RFC 1918 (`10/8`, `172.16/12`, `192.168/16`) and `.svc.cluster.local`, which
is exactly why pod and Service traffic works, but **not the VM's own interface
address**. The apiserver is Go code honoring proxy env vars, so its kubelet
dial goes to the proxy, which has no route to that address and drops it.

**A corrected hypothesis worth keeping.** The first explanation was that the
node IP sits in RFC 5737 TEST-NET-1 and the sandbox drops it. That was **wrong**,
and a `NO_PROXY` bisect on the same URL disproved it:

```bash
curl -sk https://<node-ip>:10250/healthz                                  # EOF
NO_PROXY="<node-ip>,$NO_PROXY" curl -sk https://<node-ip>:10250/healthz   # Unauthorized
```

`Unauthorized` means reachable, the kubelet answered. Same address, same TLS;
only the proxy changed.

**Fix.** Export in the *server's* environment before starting k3s:

```bash
export NO_PROXY="192.0.2.0/24,10.42.0.0/16,10.43.0.0/16,$NO_PROXY"
export no_proxy="$NO_PROXY"
```

`10.42/16` and `10.43/16` are k3s's cluster and service CIDRs, already inside
`10/8`, but stated explicitly so a changed default can't silently break pod
traffic.

**General rule.** Never disable TLS verification, never unset `HTTPS_PROXY`.
For node-local or cluster-internal addresses, extend `NO_PROXY` instead.
Anything dialling a non-standard local address, kubelets, registries on the VM
IP, NodePort from the host, needs the address excluded or the connection dies
at the proxy with an opaque EOF. Diagnose with:

```bash
curl -sS "$HTTPS_PROXY/__agentproxy/status"
```

---

## §3: Kubernetes ≥1.35 refuses cgroup v1

**Symptom.**

```
kubelet is configured to not run on a host using cgroup v1.
cgroup v1 support is unsupported and will be removed in a future release
```

Reproduced on k3s v1.35.7+k3s1 and v1.36.3+k3s1. v1.33.x runs without it.

**Fix.** `failCgroupV1: false`, a **KubeletConfiguration field, not a flag**.
`--kubelet-arg=fail-cgroup-v1=false` fails with `unknown flag`. Use a drop-in:

```bash
mkdir -p /etc/rancher/k3s/kubelet.conf.d
cat > /etc/rancher/k3s/kubelet.conf.d/00-cgroupv1.conf <<'EOF'
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
failCgroupV1: false
EOF

k3s server --kubelet-arg=config-dir=/etc/rancher/k3s/kubelet.conf.d ...
```

**Known expiry.** Upstream says cgroup v1 support "will be removed in a future
release." When that lands no kubelet knob will help, and staying on latest k3s
depends on the sandbox moving to cgroup v2.

---

## §4: Images

A k3s started with the session's proxy environment pulls from registries
directly, so this is usually unnecessary. Use `scripts/import-image.sh` when
you need Docker-side auth, want to reuse an image the host already holds, or
are pre-staging before cutting network.

The mechanism that matters: images must land in containerd's **`k8s.io`
namespace**, the one the kubelet's CRI reads. An import into the default
namespace reports success and is invisible to pods. Verify rather than trusting
an exit code:

```bash
k3s ctr -n k8s.io images ls -q | grep myimage
```

---

## §5: Native k3s first; k3d works with two fixes

Prefer native k3s: zero moving parts, and every fix in this document applies
once. But the original k3d rejection was overturned once measured, if a
tool hardcodes k3d, it can be made to work:

1. **Pulls inside node containers fail on CA trust, not reachability.**
   The proxy listens on host loopback, but node egress is transparently
   intercepted anyway; containerd inside the node fails with
   `x509: certificate signed by unknown authority` on the pause image.
   Fix at create time:

   ```bash
   k3d cluster create NAME \
     --volume /root/.ccr/ca-bundle.crt:/etc/ssl/certs/ca-certificates.crt
   ```

2. **Same runc wall, minimal node image.** Pod sandboxes inside nodes hit the
   `oom_score_adj` failure from §1, and the node image has only busybox sh, so
   the python3-based `runc-oomfix` cannot run there. Install the sed-based
   variant into each node:

   ```bash
   docker exec k3d-NAME-server-0 sh -c 'cp /bin/runc /bin/runc.real'
   docker cp "${CLAUDE_SKILL_DIR}/scripts/runc-oomfix-busybox" \
     k3d-NAME-server-0:/bin/runc
   ```

   k3d's default k3s (v1.31.x) predates the cgroup v1 refusal, so nodes need
   no kubelet drop-in.

With both fixes, all kube-system pods (including the traefik helm chain) and
user workloads run, with working `kubectl exec`. One caveat survives from the
original evaluation: **`k3d image import` silently lands nothing**: it
reports success while the node's `k8s.io` namespace holds zero images. Use
`docker save | ctr -n k8s.io images import -` inside the node instead.

## §7. kind, and a runtime that breaks podman

**kind works**, with the same fix as everything else. `kind create cluster`
fails at `kubeadm init` with the §1 `oom_score_adj` error. The kind node
image ships bash and python3, so the standard `scripts/runc-oomfix` runs
there unmodified (unlike k3d nodes, which need the busybox variant).

```bash
kind create cluster --name NAME --retain      # --retain keeps the node after the failure
docker exec NAME-control-plane sh -c 'cp /usr/local/sbin/runc /usr/local/sbin/runc.real'
docker cp "${CLAUDE_SKILL_DIR}/scripts/runc-oomfix" NAME-control-plane:/usr/local/sbin/runc
docker exec NAME-control-plane sh -c \
  'chmod +x /usr/local/sbin/runc; sed -i "s#^REAL_RUNC=.*#REAL_RUNC=\"/usr/local/sbin/runc.real\"#" /usr/local/sbin/runc'
docker exec --privileged NAME-control-plane kubeadm init --config=/kind/kubeadm.conf --skip-token-print
# CNI must be applied by hand on this path, and its manifest is a template:
docker exec NAME-control-plane sh -c \
  'sed "s|{{ .PodSubnet }}|10.244.0.0/16|g" /kind/manifests/default-cni.yaml > /tmp/cni.yaml
   KUBECONFIG=/etc/kubernetes/admin.conf kubectl apply -f /tmp/cni.yaml'
```

**Do not install `crun`.** It refuses this sandbox outright
(`cgroups in hybrid mode not supported, drop all controllers from cgroupv2`,
because of §3's cgroup v1), and `apt-get install crun` silently becomes
podman's default runtime, breaking a working podman. If it is already
present, pass `--runtime runc` or reset the default in `containers.conf`.

## §6: "Can't we just add the missing capability?"

Not for the containers that need it. `cap_sys_resource` is absent from **PID 1's
own bounding set** (`CapBnd: 000001fffeffffff`, bit 24 clear), the kernel only
ever lets a process shrink its bounding set, and **every descendant of PID 1
inherits it** — so no ordinary process, root included, regains it.
`capsh --caps=cap_sys_resource+ep` fails, and file capabilities don't work
either: exec of a `setcap cap_sys_resource+ep` binary is refused because the
file cap exceeds the bounding set.

**There is exactly one path back, and it does not help pods (exp 130).** A
`call_usermodehelper` child — spawned by the kernel from `init_cred`, *not*
descending from PID 1 — arrives with the full capability set including bit 24,
and can write a negative `oom_score_adj` and raise `RLIMIT_NOFILE`. So the
capability is not truly unreachable. But the CRI does not launch runc as a
usermodehelper; the container runtime is an ordinary PID 1 descendant, so this
route is useless for the pod path and the runc wrapper (§1) is still the only
available layer for it. What changes is the honest framing: this is a
descendant-only exclusion with a documented bypass, not an absolute one.

That one omission has wide reach, it also explains CRIU's `RLIMIT_NOFILE`
EPERM, the `--privileged` capability-mask gap, and the
**disk allowance**: 85 % of the filesystem is ext4 *reserved blocks* that
root may normally use only via `CAP_SYS_RESOURCE`, so writes stop at
`f_bavail` with space still physically free. The **stronger** gate is
`resv_strict` in the mount options, which shuts the `resuid` and `resgid`
routes regardless of their values — so the reserve stays out of reach even for
a process those values would otherwise admit.

And the capability is exactly what's being checked: **this code path** is
unpatched mainline — lowering `oom_score_adj` below the inherited floor is
gated on `CAP_SYS_RESOURCE`, the write fails with EACCES (shell `echo`
mislabels it "I/O error"), and a userns root fails too because the check is
`capable()` against the initial namespace, not `ns_capable()`. Do not
generalize that to the kernel: it is **not** unpatched overall. The root
filesystem is mounted with `resv_strict`, an ext4 option that exists in no
upstream tree and that measurably changes reserved-block behavior. PID 1's floor is 0, so an *ordinary* process cannot
reach a negative value (a usermodehelper child can, per the note above, but the
CRI does not use that path). For the container runtime, the launcher's
capability omission is the effective root cause and the runc wrapper (§1) is the
only available layer. The same missing
capability also explains CRIU's `RLIMIT_NOFILE` EPERM and the
`--privileged` capability-mask gap.
