#!/bin/sh
# Make podman default to runc.
#
# Podman's default OCI runtime here is crun, which refuses this kernel's hybrid
# cgroup layout — every `podman run` fails with:
#   crun: cgroups in hybrid mode not supported, drop all controllers from cgroupv2
# runc has no such problem. This installs a containers.conf drop-in so plain
# `podman run` works with no per-command --runtime flag. Idempotent.
# Measured on this platform.
set -eu
DROPIN=/etc/containers/containers.conf.d/10-runc.conf
mkdir -p "$(dirname "$DROPIN")"
cat > "$DROPIN" <<'CONF'
# Default to runc: crun rejects this kernel's hybrid cgroup mode.
[engine]
runtime = "runc"
CONF
runtime="$(podman info --format '{{.Host.OCIRuntime.Name}}' 2>/dev/null || echo '?')"
if [ "$runtime" = "runc" ]; then
  echo "podman default runtime is now: runc (drop-in at $DROPIN)"
else
  echo "WARNING: podman still reports runtime='$runtime' — check $DROPIN" >&2
  exit 1
fi
# Prove it by output, not status: run a container and read its stdout back.
out="$(podman run --rm docker.io/library/busybox:latest echo ok 2>/dev/null || true)"
[ "$out" = "ok" ] && echo "verified: 'podman run' works with no --runtime flag" \
                   || { echo "verify FAILED: container did not run" >&2; exit 1; }
