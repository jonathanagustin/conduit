#!/usr/bin/env bash
# Offline host-selection and container-command regression checks.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "${root}/scripts/lib/development-host.sh"
fixture="$(mktemp -d)"
trap 'rm -rf "${fixture}"' EXIT
mkdir -p "${fixture}/bin"
export CONDUIT_HOST_TEST_LOG="${fixture}/commands.log"

cat >"${fixture}/bin/uname" <<'SH'
#!/usr/bin/env bash
case "$1" in
  -s) printf '%s\n' "${CONDUIT_HOST_TEST_OS:-Linux}" ;;
  -m) printf '%s\n' "${CONDUIT_HOST_TEST_ARCH:-x86_64}" ;;
esac
SH
cat >"${fixture}/bin/podman" <<'SH'
#!/usr/bin/env bash
printf '%s %s\n' "${0##*/}" "$*" >>"${CONDUIT_HOST_TEST_LOG}"
case "$1" in
  info) [[ "${CONDUIT_HOST_TEST_ENGINE_DOWN:-0}" != 1 ]] ;;
  run) printf 'test-container\n' ;;
  port) printf '127.0.0.1:54321\n' ;;
  *) exit 0 ;;
esac
SH
cat >"${fixture}/bin/docker" <<'SH'
#!/usr/bin/env bash
if [[ "${CONDUIT_HOST_TEST_DOCKER_DOWN:-0}" == 1 ]]; then exit 1; fi
exec "$(dirname "$0")/podman" "$@"
SH
cat >"${fixture}/bin/broken-tool" <<'SH'
#!/usr/bin/env bash
exit 1
SH
cat >"${fixture}/bin/go" <<'SH'
#!/usr/bin/env bash
printf 'go-prefix=%s dsn=%s\n' "${CONDUIT_TEST_PG_TOOL_PREFIX:-}" "${CONDUIT_TEST_POSTGRES_DSN:-}" >>"${CONDUIT_HOST_TEST_LOG}"
exit "${CONDUIT_HOST_TEST_GO_FAIL:-0}"
SH
cat >"${fixture}/bin/sleep" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod 0755 "${fixture}"/bin/*
export PATH="${fixture}/bin:${PATH}"
unset CONDUIT_CONTAINER_ENGINE

if conduit_host_tool_works broken-tool --version; then
  echo 'an executable that fails was reported usable' >&2; exit 1
fi
[[ "$(conduit_container_engine)" == docker ]]
export CONDUIT_HOST_TEST_DOCKER_DOWN=1
[[ "$(conduit_container_engine)" == podman ]]
CONDUIT_CONTAINER_ENGINE="${fixture}/bin/podman"
export CONDUIT_CONTAINER_ENGINE
[[ "$(conduit_container_engine)" == "${fixture}/bin/podman" ]]
if CONDUIT_CONTAINER_ENGINE=missing-engine conduit_container_engine >/dev/null 2>&1; then
  echo 'an explicit unavailable engine fell back to another engine' >&2; exit 1
fi
if CONDUIT_CONTAINER_ENGINE='podman --remote' conduit_container_engine >/dev/null 2>&1; then
  echo 'an engine command string was accepted as an executable' >&2; exit 1
fi
if CONDUIT_HOST_TEST_ENGINE_DOWN=1 conduit_container_engine >/dev/null 2>&1; then
  echo 'an unreachable explicit engine was reported usable' >&2; exit 1
fi

for tuple in Linux:x86_64:linux_amd64 Linux:aarch64:linux_arm64 Darwin:x86_64:darwin_amd64 Darwin:arm64:darwin_arm64; do
  CONDUIT_HOST_TEST_OS="${tuple%%:*}"
  remainder="${tuple#*:}"
  CONDUIT_HOST_TEST_ARCH="${remainder%%:*}"
  platform="${remainder#*:}"
  export CONDUIT_HOST_TEST_OS CONDUIT_HOST_TEST_ARCH
  descriptor="$("${root}/scripts/fetch-temporal-cli.sh" --print-download)"
  [[ "${descriptor}" == *"/v1.10.0/temporal_cli_1.10.0_${platform}.tar.gz" ]]
  [[ "${descriptor%% *}" =~ ^[0-9a-f]{64}$ ]]
done
if CONDUIT_HOST_TEST_OS=Unsupported "${root}/scripts/fetch-temporal-cli.sh" >/dev/null 2>&1; then
  echo 'an unsupported Temporal host was accepted' >&2; exit 1
fi
unset CONDUIT_HOST_TEST_OS CONDUIT_HOST_TEST_ARCH

timestamp="$(conduit_utc_timestamp -3600)"
python3 - "${timestamp}" <<'PY'
from datetime import datetime, timezone
import sys
timestamp = datetime.strptime(sys.argv[1], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)
assert 3599 <= (datetime.now(timezone.utc) - timestamp).total_seconds() < 3690
PY

: >"${CONDUIT_HOST_TEST_LOG}"
"${root}/scripts/test-postgres-repository.sh"
grep -q '^podman run .*--publish 127.0.0.1::5432' "${CONDUIT_HOST_TEST_LOG}"
grep -q '^podman exec .* pg_isready' "${CONDUIT_HOST_TEST_LOG}"
grep -q '^podman port .* 5432/tcp' "${CONDUIT_HOST_TEST_LOG}"
grep -q "^go-prefix=${fixture}/bin/podman exec -i conduit-postgres-repository-.* dsn=postgres://conduit:conduit_test@127.0.0.1:54321/conduit_test?sslmode=disable$" "${CONDUIT_HOST_TEST_LOG}"
grep -q '^podman rm -f -v conduit-postgres-repository-' "${CONDUIT_HOST_TEST_LOG}"

: >"${CONDUIT_HOST_TEST_LOG}"
if CONDUIT_HOST_TEST_GO_FAIL=1 "${root}/scripts/test-postgres-repository.sh"; then
  echo 'a failed Go check was swallowed by the PostgreSQL gate' >&2; exit 1
fi
grep -q '^podman rm -f -v conduit-postgres-repository-' "${CONDUIT_HOST_TEST_LOG}"

cat >"${fixture}/bin/ruby" <<'SH'
#!/usr/bin/env bash
printf 'ruby %s\n' "$*" >>"${CONDUIT_HOST_TEST_LOG}"
if [[ "$1" == -rjson ]]; then
  [[ "${CONDUIT_HOST_TEST_JSON_PATCHED:-0}" == 1 ]]
  exit $?
fi
exit 99
SH
cat >"${fixture}/bin/curl" <<'SH'
#!/usr/bin/env bash
printf 'curl %s\n' "$*" >>"${CONDUIT_HOST_TEST_LOG}"
while [[ "$#" -gt 0 ]]; do
  if [[ "$1" == -o ]]; then printf 'corrupt gem' >"$2"; exit 0; fi
  shift
done
exit 1
SH
chmod 0755 "${fixture}/bin/ruby" "${fixture}/bin/curl"
: >"${CONDUIT_HOST_TEST_LOG}"
CONDUIT_HOST_TEST_JSON_PATCHED=1 MISE_CACHE_DIR="${fixture}/gem-cache" "${root}/scripts/install-ruby-json.sh" >/dev/null
if grep -q '^curl ' "${CONDUIT_HOST_TEST_LOG}"; then
  echo 'an already patched Ruby JSON runtime triggered a download' >&2; exit 1
fi
: >"${CONDUIT_HOST_TEST_LOG}"
if MISE_CACHE_DIR="${fixture}/gem-cache" "${root}/scripts/install-ruby-json.sh" >/dev/null 2>&1; then
  echo 'a corrupted JSON gem was accepted' >&2; exit 1
fi
if grep -q '^ruby -S gem ' "${CONDUIT_HOST_TEST_LOG}"; then
  echo 'an unverified JSON gem reached the installer' >&2; exit 1
fi
echo 'developer host selection, native Temporal descriptors, UTC timestamps and PostgreSQL engine forwarding passed'
