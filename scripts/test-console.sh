#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_build="$(mktemp -d)"
trap 'rm -rf "${test_build}"' EXIT

"${repo_root}/scripts/build-console.sh"
# `verify-local.sh` deliberately points XDG_CACHE_HOME at a repository-owned,
# restart-safe cache. A browser present in the ambient Codespace cache is not
# evidence that this isolated gate can bootstrap itself, so install the exact
# Playwright-managed Chromium selected by the frozen lock before launching.
# CI's preceding `--with-deps` step remains responsible for OS packages.
pnpm --dir "${repo_root}/web" exec playwright install chromium
(cd "${repo_root}" && CONDUIT_TEST_AGENT_CLIENT=1 go test -race ./internal/hub/composition -run '^TestPlanningJavaScriptClientIntegration$' -count=1)
PLATFORM_CONSOLE_OUT_DIR="${test_build}" pnpm --dir "${repo_root}/web" test
"${repo_root}/scripts/verify-console-assets.sh"
(cd "${repo_root}" && go test -race ./internal/consoleui)
