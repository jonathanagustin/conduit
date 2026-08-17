#!/usr/bin/env bash
# throttle-disk.sh — run a command under a hard disk-I/O cap.
#
# WHY THIS EXISTS
#
# Network shaping is one command here (`shape-container-net.sh`) and disk
# throttling was prose, so the degraded-hardware use case was half-packaged:
# you could simulate a 3G link but not a slow disk. Container runtimes have
# `--device-write-bps`, which only helps if the workload IS a container; this
# throttles any command — a test suite, a build, a migration.
#
# Mechanism: cgroup v1 `blkio.throttle.*`. This kernel is cgroup v1 hybrid, so
# there is no v2 `io.max` — the root `cgroup.controllers` is empty and the
# controllers are mounted v1 per-controller.
#
#   throttle-disk.sh --write 4M -- dd if=/dev/zero of=/tmp/x bs=1M count=64 oflag=direct
#   throttle-disk.sh --read 1M --write 1M -- ./my-test-suite
#   throttle-disk.sh --iops-write 100 -- ./commit-heavy-thing
#
# READ THIS BEFORE BELIEVING A NUMBER
#
# The cap governs DEVICE I/O. A buffered write that never calls fsync/fdatasync
# does not touch the device while it runs: it fills page cache and returns. A
# 200 MiB buffered write measured 2.0 GB/s under a 4 MiB/s cap, with ZERO bytes
# through the throttle, because ~3.2 GB of dirty headroom deferred the bill.
# The cap bounds SUSTAINED throughput, not burst.
#
# So: use O_DIRECT (`dd oflag=direct`) or make the workload fsync. If your
# benchmark does neither, you are measuring page cache and the throttle is
# irrelevant to it.
set -Eeuo pipefail

DEV=/dev/vda
RBPS=""; WBPS=""; RIOPS=""; WIOPS=""
usage(){ sed -n '2,32p' "$0" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }
while [ $# -gt 0 ]; do
  case "$1" in
    --read)       RBPS=${2:?}; shift 2 ;;
    --write)      WBPS=${2:?}; shift 2 ;;
    --iops-read)  RIOPS=${2:?}; shift 2 ;;
    --iops-write) WIOPS=${2:?}; shift 2 ;;
    --device)     DEV=${2:?}; shift 2 ;;
    -h|--help)    usage 0 ;;
    --)           shift; break ;;
    *) echo "throttle-disk: unknown argument $1" >&2; usage 2 ;;
  esac
done
[ $# -gt 0 ] || { echo "throttle-disk: no command given (use -- before it)" >&2; exit 2; }
[ "$(id -u)" -eq 0 ] || { echo "throttle-disk: must run as root (writes cgroup knobs)" >&2; exit 2; }

CG=/sys/fs/cgroup/blkio
[ -d "$CG" ] || { echo "throttle-disk: no v1 blkio controller at $CG" >&2; exit 2; }
[ -b "$DEV" ] || { echo "throttle-disk: $DEV is not a block device" >&2; exit 2; }

# blkio wants MAJOR:MINOR in decimal.
MAJMIN=$(python3 -c "import os,sys;s=os.stat(sys.argv[1]);print('%d:%d'%(os.major(s.st_rdev),os.minor(s.st_rdev)))" "$DEV")

to_bytes(){ # 4M -> 4194304
  case "$1" in
    *[Kk]) echo $(( ${1%[Kk]} * 1024 )) ;;
    *[Mm]) echo $(( ${1%[Mm]} * 1024 * 1024 )) ;;
    *[Gg]) echo $(( ${1%[Gg]} * 1024 * 1024 * 1024 )) ;;
    *) echo "$1" ;;
  esac
}

DIR="$CG/ccw-throttle.$$"
mkdir -p "$DIR"
cleanup(){
  # move any survivors back to the root cgroup, then remove ours. rmdir fails
  # while a task remains, which would silently leave the cap in place.
  while read -r pid; do echo "$pid" > "$CG/tasks" 2>/dev/null || true; done < "$DIR/tasks" 2>/dev/null || true
  rmdir "$DIR" 2>/dev/null || echo "throttle-disk: could not remove $DIR (tasks left?)" >&2
}
trap cleanup EXIT

set_knob(){ [ -n "$2" ] || return 0; echo "$MAJMIN $2" > "$DIR/$1" || {
  echo "throttle-disk: could not write $1" >&2; exit 1; }; }
set_knob blkio.throttle.read_bps_device   "${RBPS:+$(to_bytes "$RBPS")}"
set_knob blkio.throttle.write_bps_device  "${WBPS:+$(to_bytes "$WBPS")}"
set_knob blkio.throttle.read_iops_device  "$RIOPS"
set_knob blkio.throttle.write_iops_device "$WIOPS"

echo "throttle-disk: $DEV ($MAJMIN) read=${RBPS:-unlimited} write=${WBPS:-unlimited} iops-r=${RIOPS:-unlimited} iops-w=${WIOPS:-unlimited}"

# Verify by output: assert the knob actually took, rather than trusting write().
for k in read_bps_device write_bps_device read_iops_device write_iops_device; do
  [ -s "$DIR/blkio.throttle.$k" ] && echo "  applied: $k = $(cat "$DIR/blkio.throttle.$k")"
done

echo $$ > "$DIR/tasks"          # this shell, and therefore its children

# NOT exec: exec REPLACES this shell, so the EXIT trap never fires and the
# cgroup leaks — one per invocation, each still holding a live throttle. Caught
# by checking for leftovers after a run rather than by reading the code.
# HANDOFF 5: if you flip machine state, restore it AND verify.
set +e
"$@"
rc=$?
set -e
exit "$rc"
