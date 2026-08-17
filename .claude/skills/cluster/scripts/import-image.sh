#!/bin/bash
# Pull an image with host Docker and import it into k3s's containerd so pods
# can use it. SELF-CONTAINED: no repository layout assumed.
#
#   ./import-image.sh IMAGE [IMAGE...]
#
# Usually unnecessary — a k3s started with the session's proxy environment
# pulls from registries directly. Use this for Docker-side auth, to reuse an
# image the host already holds, or to pre-stage before cutting network.
#
# The mechanism that matters: images must land in containerd's k8s.io
# namespace, the one the kubelet's CRI reads. An import into the default
# namespace reports success and is invisible to pods.
set -euo pipefail

K3S_BIN="${K3S_BIN:-/usr/local/bin/k3s}"

[ $# -ge 1 ] || { echo "usage: $0 IMAGE [IMAGE...]" >&2; exit 2; }
command -v docker >/dev/null || { echo "docker is required" >&2; exit 2; }
docker info >/dev/null 2>&1 || { echo "docker daemon not running (sudo dockerd &)" >&2; exit 2; }
"$K3S_BIN" ctr version >/dev/null 2>&1 || { echo "k3s containerd unreachable — is the cluster up?" >&2; exit 2; }

for image in "$@"; do
  echo "import-image: pulling $image via host docker"
  docker pull -q "$image"

  echo "import-image: importing into k3s containerd (k8s.io namespace)"
  docker save "$image" | "$K3S_BIN" ctr -n k8s.io images import -

  if "$K3S_BIN" ctr -n k8s.io images ls -q | grep -qF "$image"; then
    echo "import-image: OK — $image visible to the CRI"
  else
    normalized="docker.io/library/${image}"
    case "$image" in */*) normalized="docker.io/${image}" ;; esac
    case "$image" in *.*/*|*:*/*) normalized="$image" ;; esac
    if "$K3S_BIN" ctr -n k8s.io images ls -q | grep -qF "$normalized"; then
      echo "import-image: OK — $normalized visible to the CRI"
    else
      echo "import-image: FAILED — $image not visible in k8s.io namespace" >&2
      exit 1
    fi
  fi
done
