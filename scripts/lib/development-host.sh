#!/usr/bin/env bash
# Shared host checks for the installer and focused developer gates.

conduit_host_tool_works() {
  local tool="$1"
  shift
  command -v "${tool}" >/dev/null 2>&1 || return 1
  # Apple's developer-tool stubs exist before their backing tools do. Avoid
  # triggering an installation dialog just to check the prerequisites.
  if [[ "$(uname -s)" == Darwin ]]; then
    case "$(command -v "${tool}")" in
      /usr/bin/git | /usr/bin/python3 | /usr/bin/clang | /usr/bin/cc)
        xcode-select -p >/dev/null 2>&1 || return 1
        ;;
    esac
  fi
  "${tool}" "$@" >/dev/null 2>&1
}

conduit_container_engine() {
  local candidate
  if [[ -n "${CONDUIT_CONTAINER_ENGINE:-}" ]]; then
    candidate="${CONDUIT_CONTAINER_ENGINE}"
    if [[ "${candidate}" == *[[:space:]]* ]]; then
      echo 'CONDUIT_CONTAINER_ENGINE must name one executable without whitespace' >&2
      return 1
    fi
    if conduit_host_tool_works "${candidate}" info; then
      printf '%s\n' "${candidate}"
      return 0
    fi
    echo "Container engine is unavailable: ${candidate}" >&2
    return 1
  fi
  for candidate in docker podman; do
    if conduit_host_tool_works "${candidate}" info; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done
  if [[ "$(uname -s)" == Darwin ]] && conduit_host_tool_works /opt/podman/bin/podman info; then
    printf '%s\n' /opt/podman/bin/podman
    return 0
  fi
  echo 'A running Docker or Podman engine is required; set CONDUIT_CONTAINER_ENGINE to its executable if it is outside PATH' >&2
  return 1
}

conduit_utc_timestamp() {
  python3 - "$1" <<'PY'
from datetime import datetime, timedelta, timezone
import sys
print((datetime.now(timezone.utc) + timedelta(seconds=int(sys.argv[1]))).strftime("%Y-%m-%dT%H:%M:%SZ"))
PY
}
