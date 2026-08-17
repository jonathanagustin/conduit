#!/usr/bin/env bash
# Exercise runc_dir, the resolution fix 5 exists for.
#
# Fix 5 is invisible from the outside: a wrapper installed at data/current when
# the running server resolves runc from somewhere else looks exactly like a
# working one -- present, correct, and never invoked. Nothing about that state
# is observable except by asking which directory the live process uses, so the
# resolution itself is what gets a test.
#
# The interesting case is 5: a running server whose PATH names one directory
# while data/current points at another. That is the situation that was observed
# in the field, and the pre-fix code returned the wrong answer for it.
#
# SAFETY: this fabricates /var/lib/rancher/k3s/data entries and a process whose
# command line reads "k3s server", so it refuses to run when either a real
# server or a real data directory is present rather than corrupting a cluster.
# It removes only the directories it created.
set -uo pipefail

SCRIPTS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA=/var/lib/rancher/k3s/data

[ "$(id -u)" -eq 0 ] || { echo "bootstrap-k3s.test: must run as root" >&2; exit 2; }
if pgrep -f 'k3s[ ]server' >/dev/null; then
  echo "bootstrap-k3s.test: SKIP — a k3s server is running; this test fabricates one" >&2
  exit 0
fi
if [ -e "$DATA" ]; then
  echo "bootstrap-k3s.test: SKIP — $DATA exists; refusing to disturb a real cluster" >&2
  exit 0
fi

# die is referenced by nothing in runc_dir, but keep the harness honest if that
# changes: a die that is not defined would fail as a command-not-found.
die(){ printf 'FATAL: %s\n' "$*" >&2; exit 1; }
eval "$(sed -n '/^runc_dir() {/,/^}/p' "$SCRIPTS/bootstrap-k3s.sh")"

pass=0; fail=0
probe(){ local s=0 d; d="$(runc_dir)" || s=$?; printf 'status=%s dir=%s\n' "$s" "${d:-<none>}"; }
check(){ # want, got, label
  if [ "$1" = "$2" ]; then printf '  PASS %s\n' "$3"; pass=$((pass+1))
  else printf '  FAIL %s: want %s, got %s\n' "$3" "$1" "$2"; fail=$((fail+1)); fi
}
server_pid=""
fake_server(){ # $1: PATH for the fabricated server
  env PATH="$1" setsid bash -c 'exec -a "/usr/local/bin/k3s server" sleep 30' >/dev/null 2>&1 &
  sleep 1
  server_pid="$(pgrep -f 'k3s[ ]server' | head -1)"
}
stop_server(){ [ -n "$server_pid" ] && kill "$server_pid" 2>/dev/null; server_pid=""; sleep 1; }
cleanup(){ stop_server; rm -rf "$DATA"; }
trap cleanup EXIT

LIVE="$DATA/$(printf 'a%.0s' {1..64})/bin"
STALE="$DATA/$(printf 'b%.0s' {1..64})"
mkdir -p "$LIVE" "$STALE/bin"

echo "case 1: no server, no data/current"
check "status=1 dir=<none>" "$(probe)" "reports nothing to inspect yet"

echo "case 2: no server, data/current present"
ln -sfn "${LIVE%/bin}" "$DATA/current"
check "status=0 dir=$LIVE" "$(probe)" "falls back to data/current before first start"
rm -f "$DATA/current"

echo "case 3: server running, PATH names the data directory"
fake_server "$LIVE:/usr/bin:/bin"
check "status=0 dir=$LIVE" "$(probe)" "reads the directory from the live process"
stop_server

echo "case 4: server running, PATH names no data directory"
fake_server "/usr/bin:/bin"
check "status=2 dir=<none>" "$(probe)" "refuses to guess when the layout changed"
stop_server

echo "case 5: running server disagrees with data/current — the fix-5 case"
ln -sfn "$STALE" "$DATA/current"
fake_server "$LIVE:/usr/bin:/bin"
check "status=0 dir=$LIVE" "$(probe)" "prefers the live process over the stale symlink"
stop_server

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
