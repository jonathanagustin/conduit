#!/bin/sh
# shape-container-net.sh — throttle a running container's network to a named
# slow-link profile, to test how an app behaves on a poor connection.
#
# Works by applying tc TBF to the container's HOST-SIDE veth (found via the
# container's eth0 iflink), so the container itself needs no NET_ADMIN and no
# tc/iproute2 installed. Measured precise. Host-native — no guest
# kernel needed. For latency/loss/jitter you need netem in a guest kernel
#, which the host lacks.
#
# Usage:
#   shape-container-net.sh <container> <profile>   # apply
#   shape-container-net.sh <container> clear        # remove
# Profiles (downlink rate): satellite=512kbit edge=1mbit 3g=3mbit dsl=8mbit
#   cable=50mbit  (or pass a raw tc rate like 10mbit)
set -eu
CTR="$1"; PROF="${2:-}"
runtime_exec() { podman exec "$CTR" "$@" 2>/dev/null || docker exec "$CTR" "$@"; }
IFLINK="$(runtime_exec cat /sys/class/net/eth0/iflink)"
VETH="$(ip -o link | awk -F': ' -v p="$IFLINK" '$1==p{print $2}' | sed 's/@.*//')"
[ -n "$VETH" ] || { echo "could not find host veth for container $CTR" >&2; exit 1; }
case "$PROF" in
  clear|"") tc qdisc del dev "$VETH" root 2>/dev/null || true
            echo "cleared shaping on $VETH ($CTR)"; exit 0;;
  satellite) RATE=512kbit;; edge) RATE=1mbit;; 3g) RATE=3mbit;;
  dsl) RATE=8mbit;; cable) RATE=50mbit;; *) RATE="$PROF";;
esac
tc qdisc replace dev "$VETH" root tbf rate "$RATE" burst 32kbit latency 400ms
echo "shaped $CTR downlink to $RATE (profile '$PROF') on host veth $VETH"
echo "  verify from inside: run a download and time it; clear with: $0 $CTR clear"
