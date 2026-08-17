---
name: containers
description: Run podman or docker containers with full fidelity in a Claude Code web sandbox (rootless, docker-API compatibility, compose, multi-arch, systemd containers), and know the sandbox's hard kernel limits before burning time on them.
when_to_use: Use when the user wants podman, docker, buildah, compose, testcontainers, rootless containers, multi-arch/arm64 builds, or systemd inside a container in a web session; when a process inside a container gets "connection refused" to a 127.0.0.1 proxy port; when TLS fails inside a container; when rootless podman fails on /dev/net/tun; when a GitHub tarball download returns 403; when an outbound connection to a non-web port (SMTP, database, SSH) hangs or "connects" via the proxy but never responds; when you want to test software under a slow or throttled disk (I/O-degradation, low IOPS, blkio limits); when you need a seccomp profile applied to a container or are testing syscall filtering / runtime-security behavior; or when checkpoint/restore (CRIU), IPv6, kernel modules, GPU, or nested virtualization come up, those are hard limits with definitive answers.
---

# Containers in a Claude Code web sandbox

Both podman (`apt-get install podman`) and docker (`dockerd` is preinstalled;
start with `sudo dockerd >/tmp/dockerd.log 2>&1 &`) run with **full fidelity**
here: real kernel, ext4, kernel-enforced cgroup limits (OOM kill, CPU quota,
pids), seccomp, builds, netavark/bridge networking, port publishing. Verified
end to end on this platform.

## The three proxy rules (most "broken container" reports are one of these)

1. **Host-side pulls need nothing.** The intercepting CA is already in the
   system trust store; `podman pull`/`docker pull` work with or without the
   proxy env.
2. **Unset the proxy inside containers.** Podman forwards the host's
   `https_proxy=http://127.0.0.1:<session-port>` into containers, where
   loopback is the container itself → `connection refused`. Run with
   `--http-proxy=false` (podman); egress is transparently intercepted anyway.
3. **In-container TLS needs the CA.** Images ship their own trust stores.
   Mount `/root/.ccr/ca-bundle.crt` and point the client at it
   (`SSL_CERT_FILE=/ca.crt`, `curl --cacert`). Never disable verification.

## If `podman run` fails with `crun: cgroups in hybrid mode not supported`

Podman's default runtime is **crun**, and it refuses this kernel's hybrid
cgroup layout — *every* `podman run` fails identically, even `busybox echo`
(measured). The permanent fix is a one-time drop-in that makes podman
default to **runc**:

```bash
${CLAUDE_SKILL_DIR}/scripts/set-runc-default.sh        # writes /etc/containers/containers.conf.d/10-runc.conf, verifies
```

After that, plain `podman run IMAGE ...` works with no flag. Per-command
override still works for the ad-hoc case (`podman run --runtime=/usr/bin/runc
...`), and `--runtime=/usr/local/bin/runsc` (gVisor) still selects gVisor — the
drop-in only changes the *default*. runc starts containers cleanly here
(cold-start ~0.5 s). This is the same hybrid-cgroup mode that forces the k3s
`failCgroupV1` drop-in — crun is stricter about it than runc.

## Feature recipes

| Want | Recipe |
| --- | --- |
| Plain docker | `sudo dockerd >/tmp/dockerd.log 2>&1 &`, preinstalled, no config needed |
| Rootless podman | Works after `sudo chmod 0666 /dev/net/tun` (sandbox ships it 0600; 0666 is the distro norm) |
| Docker API without dockerd | `mkdir -p /run/podman && podman system service --time=0 unix:///run/podman/podman.sock &`, then `DOCKER_HOST=unix:///run/podman/podman.sock`. `docker build` needs `DOCKER_BUILDKIT=0` and a file named `Dockerfile` |
| Compose | `docker compose` v2 works against dockerd (and the podman socket for non-BuildKit flows) |
| K8s YAML without a cluster | `podman kube play pod.yaml`, pods, infra container, hostPort all work |
| systemd in a container | `podman run --systemd=always IMAGE /lib/systemd/systemd` (debian's `systemd` pkg has no `/sbin/init`, that's `systemd-sysv`) |
| Nested podman | `--privileged --http-proxy=false` + mount the CA and set `SSL_CERT_FILE` for the inner pulls |
| arm64 / multi-arch | Install `qemu-user-static`, mount binfmt_misc, register with **literal-text escapes**: see hazard below |

## Multi-arch registration hazard (can brick every exec on the VM)

`/proc/sys/fs/binfmt_misc/register` parses `\x7f`-style escapes **as text**.
Writing real bytes (printf `\x..` expansion) truncates the magic at the first
NUL, producing a rule that matches *every* x86-64 ELF, after which every
exec, including `/bin/bash`, fails with ELOOP. Register with plain `echo`
(no `-e`):

```sh
mount -t binfmt_misc binfmt_misc /proc/sys/fs/binfmt_misc 2>/dev/null
echo ':qemu-aarch64:M::\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02\x00\xb7\x00:\xff\xff\xff\xff\xff\xff\xff\x00\xff\xff\xff\xff\xff\xff\xff\xff\xfe\xff\xff\xff:/usr/bin/qemu-aarch64-static:F' \
  > /proc/sys/fs/binfmt_misc/register
```

Verify `magic` in `/proc/sys/fs/binfmt_misc/qemu-aarch64` is 20 bytes ending
`b700` **before running anything**. If the VM is already bricked: write `0`
to `/proc/sys/fs/binfmt_misc/status` with any non-shell file-write primitive
(procfs always stats as 0 bytes, a size-check "verification failure" does
not mean the write failed). Also: `podman run --arch arm64 alpine:latest`
retags `alpine:latest` as arm64 locally; re-pull `--arch amd64` afterward.

## Hard limits: definitive, do not retry

| Feature | Status | Root cause |
| --- | --- | --- |
| CRIU checkpoint/restore | ❌ on host | Host kernel lacks `CONFIG_CHECKPOINT_RESTORE`: `kcmp(2)` → ENOSYS, no `ns_last_pid`, no sock_diag. Permanent on the host. *Escape:* an Alpine `-virt` TCG guest has `CONFIG_CHECKPOINT_RESTORE=y`, and `criu dump` produces valid checkpoint images there. (CRIU ≥4.2 needed anyway for this kernel's `vvar_vclock` vDSO; build master with `make mrproper` first.) |
| IPv6 | ❌ on host | `CONFIG_IPV6=y` but disabled at boot by `ipv6.disable=1` (see `/proc/cmdline`); the stack never initializes, there are no `net.ipv6.*` sysctls, and `/sys/module/ipv6/parameters/disable` is read-only even to root — no runtime re-enable. Trap: `getaddrinfo` still returns AF_INET6 entries, so clients that try v6 first get `errno 97 Address family not supported`; force IPv4. *Escape:* the TCG guest boots without `ipv6.disable`, so a pod on the guest node binds `::1` normally — where an IPv6-requiring workload can run |
| Kernel modules | ❌ | Monolithic kernel, no `/proc/modules` |
| Nested virtualization | ❌ | No `/dev/kvm` |
| `cap_sys_resource` | ❌ for descendants | Absent from PID 1's bounding set; bounding sets only shrink, so no descendant regains it. File caps (`setcap`) exec-refused; userns root doesn't help (initial-ns check). Explains pods' oom_score_adj EACCES (mislabeled "I/O error" by shell echo), CRIU's RLIMIT EPERM, and the `--privileged` cap gap. **A kernel-spawned `usermodehelper` child DOES hold it** (exp 130), but the CRI doesn't use that path, so the runc wrapper stands — see cluster skill `reference.md` §6 |

eBPF program loading, io_uring, blkio throttling (`--device-write-bps`),
binfmt_misc, FUSE and loop devices (`mount -o loop`) all **work**: don't
assume a sandbox restriction without testing.

## Simulating a slow disk

`--device-write-bps` covers containers. For anything that is **not** a
container — a test suite, a build, a migration — use:

```bash
${CLAUDE_SKILL_DIR}/scripts/throttle-disk.sh --write 4M -- ./my-test-suite
${CLAUDE_SKILL_DIR}/scripts/throttle-disk.sh --iops-write 100 -- ./commit-heavy-thing
```

It runs the command in a v1 `blkio.throttle.*` cgroup (this kernel is cgroup v1
hybrid, so there is no v2 `io.max`), asserts the knobs took, and removes the
cgroup afterwards. Pair it with `shape-container-net.sh` to get a slow link and
a slow disk at once.

Measured precision: 961 MB/s unthrottled → **4.2 MB/s** under `--write 4M`.

> **The number is a lie unless the workload syncs.** The cap governs *device*
> I/O. Under the same 4 MiB/s cap, a buffered `dd` with no `fdatasync` ran at
> **2.1 GB/s** — 500× over the cap — because it filled page cache and returned,
> with the bill deferred to writeback. Adding `conv=fdatasync` brought it to
> 4.1 MB/s. Use `oflag=direct` or make the workload sync; a benchmark that does
> neither is measuring page cache and the cap is irrelevant to it.

**eBPF caveat: loading is not attaching, but syscall tracing still works.**
Programs load, tracepoint families work (`tracepoint:sched:sched_process_exec`
attaches and fires), tc and cgroup hooks work (Cilium runs on them), and
**XDP attaches in both generic and native modes**. `CONFIG_KPROBES` and
`CONFIG_FTRACE_SYSCALLS` are unset, so **kprobes and the per-syscall ftrace
tracepoints are unavailable** — kprobe-first tools (Tetragon) cannot run. But
the `raw_syscalls:sys_enter`/`sys_exit` tracepoints and
`BPF_RAW_TRACEPOINT_OPEN` do work, so **modern raw-tracepoint tools do**:
Falco 0.41 `modern_ebpf` captures syscalls host-wide, and bpftrace on
`tracepoint:raw_syscalls:sys_enter` works once tracefs is mounted
(`mount -t tracefs nodev /sys/kernel/tracing`). uprobes work too. Two traps
make this look broken: the `couldn't set RLIMIT_MEMLOCK` failure is the
missing `cap_sys_resource` (the bump is unnecessary under memcg accounting —
toggle it off in the tool or `LD_PRELOAD` a `setrlimit` shim), and tracefs is
unmounted by default. Scale is fine too: 20 concurrent containers start in
~4s; RAM is a real 16 GB, 4 vCPU, `pid_max` 32768.

**File watchers (vite, webpack, `tsc --watch`, nodemon) work at scale.**
`fs/inotify/max_user_watches` is **129978** — roughly 16× a large
`node_modules` — and 8081 watches over an 8 k-directory tree were added with
no failure and events fired. The limit that actually bites first is
`fs/inotify/max_user_instances` = **128**: each watcher process takes one or
more *instances*, so many concurrent dev servers exhaust instances long before
watches. Both symptoms surface as `ENOSPC` (from `inotify_add_watch` and
`inotify_init1` respectively), which reads as "disk full" and is not. Both
sysctls are writable, so raise them rather than working around them.

**There is no syscall filtering inside the guest**: `Seccomp: 0`, no
enforcing LSM (`capability,selinux` listed, but no selinuxfs/policy). The
isolation boundary is the microVM itself, which is why ptrace, eBPF,
io_uring and privileged containers all behave like a normal root Linux box.

## Check the kernel config before probing (fastest ground truth)

`/proc/config.gz` is readable and `/proc/cmdline` shows the launcher's boot
parameters. Both are decisive and take seconds:

```sh
zcat /proc/config.gz | grep -E '^CONFIG_(FEATURE)'   # is it even compiled in?
cat /proc/cmdline                                    # ipv6.disable=1, nomodule, ...
```

Two documented limits had the wrong stated cause until they were checked
this way. Available filesystems are ext4, overlay, fuse, squashfs and tmpfs
only. btrfs, xfs, nfs and cifs are absent from the config, so no amount of
tooling will mount them.

## Network egress policy (measured, not guessed)

> **Scope: this describes a *Full-access* environment.** The sandbox has four
> network-access levels — **None**, **Trusted** (an allowlist), **Full**, and
> **Custom** — and the Default onboarding environment is **Trusted**, not Full.
> At Trusted/Custom a connection can also be dropped because the *domain* is not
> allowlisted, independent of the port rules below. The diagnose skill
> (`ccw-sandbox:diagnose`) is canonical on the access levels and on telling a
> port-drop apart from a domain-block; read it first if you are unsure which
> level you are on.

Outbound is **port-filtered, not host-filtered**: at **Full access** any host is
reachable on **TCP 80 and 443**, plus **UDP 53** for DNS. Every other port (22, 25,
53/tcp, 123, 587, 993, 3306, 5432, 8080, 8443, and so on) is **silently
dropped**, so expect hangs and timeouts, never `ECONNREFUSED`. So: no SMTP, no direct
database connections, no SSH-transport git.

**"Tunnel over 443" does not work here.** The proxy enforces the
protocol, not just the port: 443 is TLS-only and 80 is HTTP-only. A tunnelled
connection whose first client bytes are not a valid TLS ClientHello (443) or
HTTP request (80) is **reset mid-stream** — `ssh -p 443`, raw TCP, and any
non-TLS payload all die with `ECONNRESET` *after* the CONNECT appears to
succeed. The only way to carry another protocol out is to **wrap it in real
TLS** (an HTTPS or WSS endpoint on 443), which the proxy passes through
untouched. Generic corporate-proxy "just tunnel SSH over 443" advice is wrong
for this environment.

**The proxy is not a way around the port filter, and it lies about it.** A
`CONNECT host:port` for a blocked port still returns
`HTTP/1.1 200 Connection Established` immediately, after which no data ever
flows. Clients that trust that response (most HTTP stacks, `proxychains`,
JDBC-over-proxy) appear to connect and then stall until timeout. The same
optimistic 200 is returned for an *allowed* port carrying the wrong protocol,
which then resets — so the CONNECT status line proves nothing either way.
**When something "connects" through the proxy but never responds, check the
port against the allowlist before debugging the application.**

Two more network limits that masquerade as bugs:

- **A request-header size wall (path- and origin-dependent — there is no single
  number).** Big headers can fail: on the **direct** path the gateway returns
  `431 Request Header Fields Too Large` (bisected once at ~32 KiB); on the
  **proxied** CONNECT path the tunnel is opaque, so the first wall you hit is the
  *origin's* (e.g. ~128 KiB Fastly, ~8 KiB api.github.com), and one direct-path
  measurement of ~64 KiB (`curl (55) Failed sending HTTP request`, a *send*
  error) has since been re-measured as **per-origin and drifting within 24 h**
  (exp 69 / T9 — no universal ceiling). Treat any specific figure as one
  origin on one day, not a constant; the diagnose skill is canonical on the
  32 KiB direct-gateway `431`. What is stable: it is a *total* header budget, not
  per-header, and request **bodies are unaffected** (1 MB posts fine). Large
  JWTs, cookie jars, or tracing headers can cross it.
- **Post-quantum TLS is fine**: Go 1.24's default `X25519MLKEM768`
  negotiates cleanly here, so don't blame PQ ClientHellos. But system
  OpenSSL 3.0.13 predates ML-KEM, so `openssl s_client -groups
  X25519MLKEM768` fails *locally* with "group cannot be set", a client
  version limit, not a network rejection.

## Fetching sources for from-source builds

The session proxy 403s GitHub release/archive **tarball URLs** for
repositories outside the session scope, but serves anonymous **`git clone`**
of any public repository:

```sh
git clone --depth 1 --branch TAG https://github.com/OWNER/REPO /workspace/OWNER/REPO
```

(This is how a from-source build can get its sources after a tarball
fetch is refused with a 403 policy message.)

> Sandbox facts have a shelf life. This environment was swapped once already
> (gVisor → Firecracker), invalidating prior research. If observation
> contradicts this text, re-measure; the text is the more likely stale party.

## Stricter isolation: gVisor runs nested here

`runsc` works inside this sandbox on both the `systrap` and `ptrace`
platforms, with no `/dev/kvm` needed, and plugs into podman directly
:

```sh
curl -sfLo /usr/local/bin/runsc https://storage.googleapis.com/gvisor/releases/release/latest/x86_64/runsc
chmod +x /usr/local/bin/runsc
podman run --rm --runtime /usr/local/bin/runsc IMAGE ...
```

Useful for an extra isolation layer, and for reproducing the *old* gVisor
sandbox's constraints on demand. `systemd-nspawn` and LXC do not work: the
host's PID 1 is the harness, not systemd.

## Storage: no device-mapper *mechanism* — but the capabilities are reachable

`CONFIG_BLK_DEV_DM` is unset, so there is **no device-mapper**, and no way to
load a module to add it. What that removes is the *mechanism*: live
`dm-crypt`/`dm-thin`/LVM block devices and software RAID, plus any CSI driver
that assumes them. Trap: `cryptsetup luksFormat` **succeeds** (it only writes a
header) and `luksOpen` then fails with `Cannot initialize device-mapper`.

Do **not** read "no device-mapper" as "no encryption / no snapshots / no thin
provisioning" — that conflates a mechanism with a capability. Each of those is
reachable another way (see `docs/USE-CASES.md` for the worked commands):

- **Encryption at rest** — ext4 native `fscrypt` (`CONFIG_FS_ENCRYPTION=y`;
  `mkfs.ext4 -O encrypt`), or a userspace LUKS image via `qemu-img`.
- **Snapshots** — `qcow2` internal snapshots (offline).
- **Thin provisioning** — sparse files / `qcow2` grow-on-write.

What is genuinely gone is the *live block-device* form of all three (a running
`dm-crypt` device, live `dm-thin` pools, LVM) and any CSI driver built on them.

## WASM

`wasmtime` runs WASI modules standalone with no special setup. The containerd WASM shim (runwasi) works fine through `ctr` and other
non-CRI paths, verified end to end on cgroup v1. **Under Kubernetes it
fails** with an opaque `io error`, because the CRI pins `oomScoreAdj: -998`
and runwasi embeds Youki's libcontainer as a library rather than exec'ing
the `runc` binary, so `runc-oomfix` never gets to clamp it. Do not
write the containerd runtime block by hand either: k3s auto-detects the shim
and a duplicate table breaks containerd entirely.

`systemd-nspawn` and LXC both fail for the same non-container reason: the
host's PID 1 is the harness, not systemd. LXC additionally defaults to a
`lxcbr0` bridge that `lxc-net` would normally create; set
`lxc.net.0.type = none` in `/etc/lxc/default.conf` **before** `lxc-create`,
and use the `download` template rather than `busybox`.

## Escape hatch: a nested VM (last resort)

There is no `/dev/kvm`, but **qemu TCG emulation works**, and a guest kernel
you choose has none of the limits above: full cgroup v2, a complete
capability set including `cap_sys_resource`, module loading, and whatever
else its config enables.

Cost depends on workload shape (same binary both sides):
**17 to 18x for branchy interpreted code**, but only **1.4x for a tight
compute loop over bulk data**. Kernel boot to shell is about 3.4 s. Use it
only when a task genuinely needs a different kernel, such as loading a
module or running CRIU.

```sh
qemu-system-x86_64 -accel tcg -m 1024 -smp 2 -nographic -display none \
  -kernel vmlinuz-virt -initrd initramfs-virt -append "console=ttyS0"
```
