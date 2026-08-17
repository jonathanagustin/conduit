#!/bin/bash
# Bootstrap a fully functional k3s cluster inside a Claude Code web sandbox.
#
# SELF-CONTAINED: depends only on this script, the runc-oomfix wrapper sitting
# beside it, and standard tooling (curl, docker not required). No repository
# layout is assumed — drop this directory anywhere.
#
# It applies five fixes the sandbox needs (see SKILL.md for the evidence):
#   1. Negative oom_score_adj writes fail (EACCES; CAP_SYS_RESOURCE absent)
#      while the CRI pins -998 on every pod sandbox -> shadow runc with a
#      clamping wrapper.
#   2. The session egress proxy eats apiserver->kubelet dials because the VM's
#      own IP is not in no_proxy -> extend NO_PROXY before start.
#   3. Kubernetes >=1.35 refuses cgroup v1 -> failCgroupV1: false via a kubelet
#      config drop-in (a config FIELD; --kubelet-arg=fail-cgroup-v1=false is an
#      unknown flag).
#   4. The allowance-shaped disk reads as DiskPressure under the default
#      percentage eviction threshold -> absolute thresholds via a drop-in.
#   5. The runc that must be wrapped is the one the RUNNING server resolves,
#      which is not necessarily data/current/bin/runc -> read the directory
#      from the live process. See runc_dir.
#
# DESIGN: repairs run UNCONDITIONALLY and BEFORE any health gate.
# Two real bugs came from the opposite order — a gate that enumerated proxies
# for health ("runc.real exists", "node is Ready") short-circuited past the
# repair code while every pod failed. Enumerated gates admit unlisted broken
# states; the only structural fix is that everything repairable is repaired
# before any short-circuit exists (the repairs are idempotent file operations
# costing milliseconds), and the gate decides just one thing: whether the
# server process needs (re)starting.
#
#   --verify   after bootstrap, prove the OUTCOME: run a pod and read its logs
#              back. This is the only check that cannot be fooled by a proxy.
set -euo pipefail

K3S_VERSION="${K3S_VERSION:-v1.36.3+k3s1}"
K3S_BIN="${K3S_BIN:-/usr/local/bin/k3s}"
SKILL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WRAPPER="${RUNC_WRAPPER:-$SKILL_DIR/runc-oomfix}"
LOG="${K3S_LOG:-/tmp/k3s-bootstrap.log}"
VERIFY=0
[ "${1:-}" = "--verify" ] && VERIFY=1

log() { printf 'k3s bootstrap: %s\n' "$*"; }
die() { printf 'k3s bootstrap: FATAL: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "must run as root"
[ -f "$WRAPPER" ] || die "runc wrapper not found beside this script at $WRAPPER"

is_elf()    { head -c 4 "$1" 2>/dev/null | grep -q ELF; }
is_wrapper(){ grep -q runc-oomfix "$1" 2>/dev/null; }

# ── fix 2 (unconditional): keep cluster traffic away from the egress proxy ────
primary_ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
[ -n "$primary_ip" ] || die "could not determine primary IP"
extra_noproxy="${primary_ip%.*}.0/24,10.42.0.0/16,10.43.0.0/16"
case ",${NO_PROXY:-}," in
  *",$extra_noproxy,"*) ;;
  *) export NO_PROXY="$extra_noproxy,${NO_PROXY:-}" no_proxy="$extra_noproxy,${no_proxy:-}" ;;
esac
log "NO_PROXY extended with $extra_noproxy"

# ── fixes 3+4 (unconditional): kubelet drop-ins are idempotent file writes ────
mkdir -p /etc/rancher/k3s/kubelet.conf.d
cat > /etc/rancher/k3s/kubelet.conf.d/00-cgroupv1.conf <<'EOF'
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
failCgroupV1: false
EOF
# evictionHard REPLACES kubelet's default map, so memory.available must be
# restated. Absolute values because the disk is an allowance (85% ext4
# reserve): percentage thresholds read a healthy allowance as disk pressure.
cat > /etc/rancher/k3s/kubelet.conf.d/01-eviction.conf <<'EOF'
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
evictionHard:
  memory.available: "100Mi"
  nodefs.available: "500Mi"
  imagefs.available: "500Mi"
  nodefs.inodesFree: "5%"
evictionPressureTransitionPeriod: "30s"
EOF

# ── fix 1 (unconditional): make the extracted runc state coherent ─────────────
# Valid end state: runc is the wrapper AND runc.real is a pristine ELF. Every
# other combination is repaired here, BEFORE any health gate can skip it:
#   runc=ELF, real=ELF      wrapper lost (upgrade/restore/exp-58 restart) -> rewrap
#   runc=ELF, real absent   never wrapped -> wrap
#   runc=wrapper, real bad  wrapper points at nothing (or at a copied wrapper,
#                           which exec-loops and wedges containerd) -> the real
#                           binary is unrecoverable; purge the extracted data
#                           dir so a fresh server re-extracts pristine binaries.
#                           Cluster state and images live under agent/ and are
#                           preserved.
purge_data_dir() { # $1: the bin directory whose runc is unrecoverable, if known
  log "unrecoverable runc state; purging extracted data dir for re-extraction"
  local resolved="${1:-}"
  # SIGTERM first and give kine time to close its sqlite WAL cleanly: the root
  # fs has no journal, and a -9 mid-write left kine wedged with
  # etcd-readiness failing until the next clean restart. Force only
  # what survives the grace period.
  pkill -TERM -f 'k3s[ ]server' 2>/dev/null || true
  for _ in 1 2 3 4 5 6 7 8; do pgrep -f 'k3s[ ]server' >/dev/null || break; sleep 1; done
  pkill -9 -f 'k3s[ ]server' 2>/dev/null || true
  pkill -9 -f 'containerd-shim-runc-v2' 2>/dev/null || true
  pkill -9 -f 'runc\.real' 2>/dev/null || true
  fuser -k -KILL /var/lib/rancher/k3s/agent/containerd/io.containerd.metadata.v1.bolt/meta.db 2>/dev/null || true
  sleep 2
  # The directory the running server actually resolved, when the caller knew it
  # -- removing only data/current would leave the broken binaries in place for a
  # server whose PATH points elsewhere, which is fix 5 applied to the purge.
  [ -n "$resolved" ] && rm -rf "${resolved%/bin}"
  local d; d="$(readlink -f /var/lib/rancher/k3s/data/current 2>/dev/null || true)"
  [ -n "$d" ] && rm -rf "$d"
  rm -rf /var/lib/rancher/k3s/data/current
  # /run/k3s holds live overlay/rootfs mounts while pods run; rm on a mountpoint
  # returns EBUSY and under set -e that killed the whole purge mid-way.
  # Lazy-unmount everything under it first, then best-effort remove — a fresh
  # server recreates whatever is missing.
  awk '$2 ~ "^/run/k3s/" {print $2}' /proc/mounts | sort -r | while read -r m; do
    umount -l "$m" 2>/dev/null || true
  done
  rm -rf /run/k3s 2>/dev/null || true
}
# ── fix 5: wrap the runc the RUNNING server resolves, not data/current ────────
#
# k3s extracts its bundled binaries to data/<sha256>/bin and puts THAT directory
# on its own PATH; containerd's config sets no BinaryName, so the shim resolves
# "runc" from that PATH. data/current is a symlink that can point somewhere else
# entirely once more than one k3s version has run -- and then the wrapper is
# installed where nothing reads it while every pod keeps failing with the fix
# apparently applied. Observed exactly that: wrapper present, current correct,
# server newer than the wrapper, and zero wrapper invocations.
#
# So the directory is read from the live process whenever there is one. Before
# the first start there is no process and data/current is all that exists, which
# makes it correct then and only then.
#
# Exit status, because a command substitution cannot die: 0 with the directory
# on stdout, 1 when there is nothing to inspect yet, 2 when a server is running
# and its PATH names no k3s data directory -- a layout change, where guessing
# data/current is exactly what reinstates the silent failure.
runc_dir() {
  local pid dir
  pid="$(pgrep -f 'k3s[ ]server' | head -1 || true)"
  if [ -n "$pid" ]; then
    dir="$(tr '\0' '\n' < "/proc/$pid/environ" 2>/dev/null |
      sed -n 's/^PATH=//p' | tr ':' '\n' |
      grep -E '^/var/lib/rancher/k3s/data/[0-9a-f]{64}/bin$' | head -1 || true)"
    [ -n "$dir" ] || return 2
    printf '%s\n' "$dir"
    return 0
  fi
  dir="$(readlink -f /var/lib/rancher/k3s/data/current/bin 2>/dev/null || true)"
  [ -n "$dir" ] && [ -d "$dir" ] || return 1
  printf '%s\n' "$dir"
}

ensure_runc() { # 0 coherent, 1 nothing to repair yet, 2 layout changed
  local dir status=0 r
  dir="$(runc_dir)" || status=$?
  [ "$status" -eq 0 ] || return "$status"
  r="$dir/runc"
  [ -f "$r" ] || return 1
  if is_wrapper "$r"; then
    if [ -f "$r.real" ] && is_elf "$r.real"; then
      return 0                          # coherent: wrapper + pristine real
    fi
    purge_data_dir "$dir"; return 1     # wrapper with no/bad real: unrecoverable in place
  fi
  if is_elf "$r"; then
    # wrapper lost. If a pristine runc.real survives, keep it; else create it.
    if [ ! -f "$r.real" ] || ! is_elf "$r.real"; then cp -a "$r" "$r.real"; fi
    install -m0755 "$WRAPPER" "$r"
    sed -i "s#^REAL_RUNC=.*#REAL_RUNC=\"\${RUNC_REAL:-$r.real}\"#" "$r"
    log "runc wrapper (re)installed at $r"
    return 0
  fi
  purge_data_dir "$dir"; return 1       # runc is neither wrapper nor ELF
}

# Same, but a layout change is fatal rather than a status nobody reads.
ensure_runc_or_die() {
  local status=0
  ensure_runc || status=$?
  [ "$status" -eq 2 ] &&
    die "a server is running and its PATH names no k3s data directory; layout changed"
  return "$status"
}
ensure_runc_or_die || true   # data dir may not exist yet; post-start pass handles it

# ── gate: decides ONLY whether the server needs (re)starting ─────────────────
# All file-level repairs above have already run. /readyz alone lies (a server
# with a dead kubelet keeps serving it) so a Ready node is also required.
if "$K3S_BIN" kubectl get --raw /readyz >/dev/null 2>&1 &&
   [ "$("$K3S_BIN" kubectl get nodes --no-headers 2>/dev/null | awk '$2=="Ready"' | wc -l)" -ge 1 ] &&
   ensure_runc_or_die; then
  log "cluster healthy (repairs, if any, already applied)"
  if [ "$VERIFY" = 1 ]; then :; else exit 0; fi
else

# ── binary ───────────────────────────────────────────────────────────────────
if ! "$K3S_BIN" --version 2>/dev/null | grep -q "$K3S_VERSION"; then
  log "downloading k3s $K3S_VERSION"
  curl -sfL --retry 3 -o "$K3S_BIN.tmp" \
    "https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION//+/%2B}/k3s" \
    || die "download failed"
  chmod +x "$K3S_BIN.tmp" && mv "$K3S_BIN.tmp" "$K3S_BIN"
fi
log "k3s $("$K3S_BIN" --version | head -1)"

# ── start ────────────────────────────────────────────────────────────────────
if ! pgrep -f 'k3s[ ]server' >/dev/null; then
  log "starting k3s server (log: $LOG)"
  nohup setsid env NO_PROXY="$NO_PROXY" no_proxy="$no_proxy" \
    "$K3S_BIN" server \
    --kubelet-arg=config-dir=/etc/rancher/k3s/kubelet.conf.d \
    --disable=traefik,metrics-server \
    --write-kubeconfig-mode=644 </dev/null >>"$LOG" 2>&1 &
  disown
fi

# ── fixes 1+5, post-start pass: wrap the runc the server resolves ─────────────
# The directory comes from the running process now, so this waits for the server
# to have extracted and exported it rather than for a symlink to appear.
data_bin=""
for _ in $(seq 1 60); do
  status=0
  data_bin="$(runc_dir)" || status=$?
  [ "$status" -eq 2 ] &&
    die "the running server's PATH names no k3s data directory; layout changed"
  [ "$status" -eq 0 ] && [ -f "$data_bin/runc" ] && break
  data_bin=""
  sleep 2
done
[ -n "$data_bin" ] || die "k3s data dir never appeared; see $LOG"
log "server resolves runc from $data_bin"
ensure_runc_or_die || die "runc state incoherent even after re-extraction; see $LOG"

# ── readiness ────────────────────────────────────────────────────────────────
for _ in $(seq 1 60); do
  "$K3S_BIN" kubectl get --raw /readyz >/dev/null 2>&1 && break
  sleep 3
done
"$K3S_BIN" kubectl get --raw /readyz >/dev/null 2>&1 || die "apiserver never ready; see $LOG"

ready=0
for _ in $(seq 1 60); do
  ready="$("$K3S_BIN" kubectl get nodes --no-headers 2>/dev/null | awk '$2=="Ready"' | wc -l)"
  [ "$ready" -ge 1 ] && break
  sleep 3
done
[ "$ready" -ge 1 ] || die "node never became Ready; see $LOG"

log "node Ready; waiting for kube-system pods"
"$K3S_BIN" kubectl -n kube-system wait --for=condition=Ready pod --all --timeout=180s >/dev/null 2>&1 \
  || log "warning: some kube-system pods not Ready yet (may still be pulling)"

log "done — KUBECONFIG=/etc/rancher/k3s/k3s.yaml"
"$K3S_BIN" kubectl get nodes
fi   # end of not-healthy branch

# ── --verify: the outcome proof no proxy can fool ────────────────────────────
# The one check equivalent to "the cluster works": create a pod, read its logs
# back through the apiserver->kubelet path, and require the expected output.
# LIMIT (by design): a corrupted server datastore is NOT healed here — that
# repair is destructive (state reset) and must be a human decision. This
# verify fails loudly instead.
if [ "$VERIFY" = 1 ]; then
  vname="ccw-verify-$$"
  "$K3S_BIN" kubectl delete pod "$vname" --now >/dev/null 2>&1 || true
  # Pod CREATION must be retried, not just the log read. On a cold start the
  # node goes Ready before the controller-manager has created the namespace's
  # default ServiceAccount, and `kubectl run` is refused until it exists —
  # measured at ~12 s behind node-Ready on a from-nothing bootstrap.
  created=0
  for _ in $(seq 1 20); do
    if "$K3S_BIN" kubectl run "$vname" --image=busybox:1.37 --restart=Never \
         --command -- echo ccw-verified >/dev/null 2>&1; then created=1; break; fi
    sleep 3
  done
  [ "$created" = 1 ] || die "verify: pod create refused for 60s; see $LOG"
  out=""
  # 3 min: right after a full purge-rebuild the first pod waits out image pull
  # and kube-system convergence; 90 s produced a false verify failure.
  for _ in $(seq 1 60); do
    out="$("$K3S_BIN" kubectl logs "$vname" 2>/dev/null || true)"
    [ "$out" = "ccw-verified" ] && break
    sleep 3
  done
  "$K3S_BIN" kubectl delete pod "$vname" --now >/dev/null 2>&1 || true
  [ "$out" = "ccw-verified" ] || die "verify FAILED: pod ran but logs did not read back; see $LOG"
  log "verified: pod executed and logs read back"
fi
