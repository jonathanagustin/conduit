#!/usr/bin/env bash
# Foreground Temporal dev server for local packaging work. Loopback only,
# persistent sqlite, UI news fetch disabled. Not a production server.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cli="${CONDUIT_TEMPORAL_CLI:-${root}/.cache/temporal/temporal}"
if [[ ! -x "${cli}" ]]; then
  "${root}/scripts/fetch-temporal-cli.sh"
  cli="${root}/.cache/temporal/temporal"
fi
db="${PACKAGING_TEMPORAL_DB:-${root}/.packaging-state/temporal.db}"
mkdir -p "$(dirname "${db}")"
exec "${cli}" server start-dev \
  --ip 127.0.0.1 \
  --ui-ip 127.0.0.1 \
  --ui-disable-news-fetch \
  --db-filename "${db}" \
  --port "${PACKAGING_TEMPORAL_PORT:-7233}" \
  --ui-port "${PACKAGING_TEMPORAL_UI_PORT:-8233}" \
  --log-level warn
