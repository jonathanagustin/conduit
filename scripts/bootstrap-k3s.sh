#!/usr/bin/env bash
# Bring up a real Kubernetes cluster for exercising the delivery path end to end.
#
# Why this is in the repository rather than in someone's shell history: the
# Agent applies Helm releases to a cluster, and until this existed there was no
# cluster to apply them to in a sandbox. The delivery path could be tested as
# far as "helm was invoked" and no further.
#
# Derived from the ccw-sandbox research at
# https://github.com/jonathanagustin/claude-code-on-the-web, which established
# the four sandbox fixes below and their evidence. What is added here is fix 5,
# which that script's own approach misses -- see below.
#
# The fixes, and what each is for:
#
#   1. Negative oom_score_adj writes fail on this kernel while the CRI pins
#      -998 on every pod sandbox, so runc's child dies at setup with
#      "can't get final child's PID from pipe: EOF". Shadow runc with a wrapper
#      that clamps the OCI spec to 0. Verified on this kernel: writing 0 to
#      /proc/self/oom_score_adj succeeds and writing -998 is denied.
#   2. The session egress proxy eats apiserver->kubelet dials because the VM's
#      own address is not in no_proxy.
#   3. Kubernetes >=1.35 refuses cgroup v1, and this sandbox has only v1.
#      failCgroupV1 is a kubelet config FIELD; there is no equivalent flag.
#   4. The allowance-shaped disk reads as DiskPressure under the default
#      percentage eviction thresholds.
#   5. The runc that must be wrapped is the one the RUNNING server resolves,
#      which is not necessarily data/current/bin/runc.
#
#      Fix 5 is the one this script exists to add. k3s extracts its bundled
#      binaries to data/<hash>/bin and puts that directory on its own PATH;
#      containerd's config sets no BinaryName, so the shim resolves "runc" from
#      that PATH. data/current is a symlink that can point somewhere else
#      entirely once more than one k3s version has run -- and then the wrapper
#      is installed where nothing reads it while every pod keeps failing with
#      the fix apparently applied. Observed exactly that: wrapper present,
#      current correct, server newer than the wrapper, and zero wrapper
#      invocations. The data directory is therefore read from the running
#      process, and the wrapper is verified to have been CALLED rather than
#      merely to exist.
#
# --verify runs a pod and reads its logs back. That is the only check that
# cannot be satisfied by a status field, which is the failure mode every other
# check here has.
#
# For the Docker daemon -- which verify-local.sh's PostgreSQL gate needs and
# which is absent by default -- see start-docker.sh beside this script. It needs
# none of the fixes below; it just is not running.
set -euo pipefail

# v1.36.3 is what this was verified against here, and it is the version that
# needs fix 3 -- its kubelet refuses cgroup v1 outright. v1.31.5 also works and
# does not need that fix, which is why it was the first thing tried.
K3S_VERSION="${K3S_VERSION:-v1.36.3+k3s1}"
K3S_BIN="${K3S_BIN:-/usr/local/bin/k3s}"
KUBECONFIG_PATH=/etc/rancher/k3s/k3s.yaml
LOG="${K3S_LOG:-/tmp/k3s-bootstrap.log}"
VERIFY=0
[ "${1:-}" = "--verify" ] && VERIFY=1

log() { printf 'k3s: %s\n' "$*"; }
die() { printf 'k3s: FATAL: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "must run as root"

# ── fix 2: keep cluster-internal traffic away from the egress proxy ───────────
CLUSTER_NO_PROXY="192.0.2.0/24,10.42.0.0/16,10.43.0.0/16,localhost,127.0.0.1,.svc,.cluster.local"
NO_PROXY="${CLUSTER_NO_PROXY},${NO_PROXY:-}"
no_proxy="${NO_PROXY}"
export NO_PROXY no_proxy

# ── fix 3 and 4: kubelet configuration the sandbox needs ─────────────────────
mkdir -p /etc/rancher/k3s/kubelet.conf.d
cat > /etc/rancher/k3s/kubelet.conf.d/00-sandbox.conf <<'EOF'
apiVersion: kubelet.config.k8s.io/v1beta1
kind: KubeletConfiguration
failCgroupV1: false
evictionHard:
  imagefs.available: "300Mi"
  nodefs.available: "300Mi"
EOF

if [ ! -x "${K3S_BIN}" ]; then
  log "downloading k3s ${K3S_VERSION}"
  curl -sfL "https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION//+/%2B}/k3s" \
    -o "${K3S_BIN}" --max-time 300 || die "download k3s"
  chmod +x "${K3S_BIN}"
fi

if ! pgrep -f "${K3S_BIN} server" >/dev/null 2>&1; then
  log "starting server (log: ${LOG})"
  nohup setsid env NO_PROXY="${NO_PROXY}" no_proxy="${no_proxy}" \
    "${K3S_BIN}" server \
    --kubelet-arg=config-dir=/etc/rancher/k3s/kubelet.conf.d \
    --disable=traefik,metrics-server \
    --write-kubeconfig-mode=644 </dev/null >>"${LOG}" 2>&1 &
  disown
fi

for _ in $(seq 1 90); do
  [ -r "${KUBECONFIG_PATH}" ] && "${K3S_BIN}" kubectl get nodes >/dev/null 2>&1 && break
  sleep 2
done
export KUBECONFIG="${KUBECONFIG_PATH}"
"${K3S_BIN}" kubectl get nodes >/dev/null 2>&1 || die "server did not become reachable; see ${LOG}"

# ── fix 1 and 5: wrap the runc the running server actually resolves ───────────
#
# Read from the live process rather than from data/current, which is the whole
# point of fix 5. If the server's PATH names no k3s data directory the layout
# has changed and guessing would reinstate the silent failure this avoids.
server_pid="$(pgrep -f "${K3S_BIN} server" | head -1)"
[ -n "${server_pid}" ] || die "no running server to read a data directory from"
data_bin="$(tr '\0' '\n' < "/proc/${server_pid}/environ" 2>/dev/null |
  sed -n 's/^PATH=//p' | tr ':' '\n' |
  grep -E '^/var/lib/rancher/k3s/data/[0-9a-f]{64}/bin$' | head -1 || true)"
[ -n "${data_bin}" ] || die "the running server's PATH names no k3s data directory; layout changed"
log "server resolves runc from ${data_bin}"

if head -c 4 "${data_bin}/runc" 2>/dev/null | grep -q ELF; then
  log "installing the oom_score_adj wrapper"
  cp "${data_bin}/runc" "${data_bin}/runc.real"
  cat > "${data_bin}/runc" <<EOF
#!/usr/bin/env bash
# Clamp negative oomScoreAdj to 0 before delegating: this kernel denies a
# negative write to /proc/self/oom_score_adj, and the CRI pins -998 on every
# pod sandbox. Inside a single-tenant VM the OOM-protection intent is moot --
# there is nothing else competing to be killed.
set -euo pipefail
REAL="${data_bin}/runc.real"
head -c 4 "\$REAL" 2>/dev/null | grep -q ELF || {
  printf 'runc wrapper: %s is not the real runc (double wrap?)\n' "\$REAL" >&2; exit 1; }
clamp() {
  python3 - "\$1" <<'PY' 2>/dev/null || true
import json, sys
try:
    with open(sys.argv[1]) as handle:
        document = json.load(handle)
except Exception:
    sys.exit(0)
changed = False
process = document.get("process")
if isinstance(process, dict) and isinstance(process.get("oomScoreAdj"), int) and process["oomScoreAdj"] < 0:
    process["oomScoreAdj"] = 0
    changed = True
if isinstance(document.get("oomScoreAdj"), int) and document["oomScoreAdj"] < 0:
    document["oomScoreAdj"] = 0
    changed = True
if changed:
    with open(sys.argv[1], "w") as handle:
        json.dump(document, handle)
PY
}
bundle="" process_document=""
arguments=("\$@")
for ((index = 0; index < \${#arguments[@]}; index++)); do
  case "\${arguments[\$index]}" in
    --bundle|-b) bundle="\${arguments[\$((index + 1))]:-}" ;;
    --bundle=*) bundle="\${arguments[\$index]#--bundle=}" ;;
    --process|-p) process_document="\${arguments[\$((index + 1))]:-}" ;;
    --process=*) process_document="\${arguments[\$index]#--process=}" ;;
  esac
done
for argument in "\$@"; do
  case "\$argument" in
    create|run) [ -f "\${bundle:-.}/config.json" ] && clamp "\${bundle:-.}/config.json"; break ;;
  esac
done
[ -n "\$process_document" ] && [ -f "\$process_document" ] && clamp "\$process_document"
[ -n "\${K3S_RUNC_WRAPPER_LOG:-}" ] && printf 'called\n' >> "\$K3S_RUNC_WRAPPER_LOG"
exec "\$REAL" "\$@"
EOF
  chmod 0755 "${data_bin}/runc"
  # Pods that failed before the wrapper existed do not retry into it.
  "${K3S_BIN}" kubectl delete pod -n kube-system --all --force --grace-period=0 >/dev/null 2>&1 || true
else
  log "wrapper already installed"
fi

if [ "${VERIFY}" -eq 0 ]; then
  log "done — export KUBECONFIG=${KUBECONFIG_PATH} (re-run with --verify to prove a pod runs)"
  exit 0
fi

# ── verify by outcome ────────────────────────────────────────────────────────
log "verifying by running a pod and reading its logs back"
# A reachable API server is not an API server that will admit a pod: the node
# has to register Ready and the namespace's default ServiceAccount has to exist
# before a create succeeds. Waiting on "kubectl get nodes" alone made this fail
# fourteen seconds in, with everything actually working.
for _ in $(seq 1 60); do
  ready="$("${K3S_BIN}" kubectl get node -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)"
  account="$("${K3S_BIN}" kubectl get serviceaccount default -o name 2>/dev/null || true)"
  [ "${ready}" = "True" ] && [ -n "${account}" ] && break
  sleep 2
done
[ "${ready}" = "True" ] || die "the node never became Ready; see ${LOG}"
[ -n "${account}" ] || die "the default ServiceAccount never appeared; see ${LOG}"

"${K3S_BIN}" kubectl delete pod k3s-bootstrap-proof --force --grace-period=0 >/dev/null 2>&1 || true
created=0
for _ in $(seq 1 10); do
  if "${K3S_BIN}" kubectl run k3s-bootstrap-proof --image=busybox:1.36 --restart=Never \
    --command -- sh -c 'echo POD-EXECUTED-OK' >/dev/null 2>&1; then
    created=1
    break
  fi
  sleep 3
done
[ "${created}" -eq 1 ] || die "could not create the proof pod; see ${LOG}"
phase=""
for _ in $(seq 1 60); do
  phase="$("${K3S_BIN}" kubectl get pod k3s-bootstrap-proof -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  [ "${phase}" = "Succeeded" ] && break
  [ "${phase}" = "Failed" ] && break
  sleep 5
done
logs="$("${K3S_BIN}" kubectl logs k3s-bootstrap-proof 2>&1 || true)"
"${K3S_BIN}" kubectl delete pod k3s-bootstrap-proof --force --grace-period=0 >/dev/null 2>&1 || true
[ "${phase}" = "Succeeded" ] || die "the proof pod ended in ${phase:-<no phase>}; see ${LOG}"
printf '%s' "${logs}" | grep -q POD-EXECUTED-OK ||
  die "the proof pod ran but its logs did not read back (got: ${logs})"
log "verified: a pod ran and its logs read back"
log "done — export KUBECONFIG=${KUBECONFIG_PATH}"
