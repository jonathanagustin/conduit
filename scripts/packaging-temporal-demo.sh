#!/usr/bin/env bash
# Re-run the localhost crash/restart evidence. Requires the pinned CLI from
# scripts/fetch-temporal-cli.sh. This does not start a Hub and does not publish.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cli="${CONDUIT_TEMPORAL_CLI:-${root}/.cache/temporal/temporal}"
if [[ ! -x "${cli}" ]]; then
  "${root}/scripts/fetch-temporal-cli.sh"
fi
cd "${root}"
CONDUIT_TEMPORAL_CLI="${cli}" go test -count=1 ./internal/packagingworkflow/pipeline/ \
  -run TestDevServerRestartRecoversWaitAndHandoff -timeout 180s -v
