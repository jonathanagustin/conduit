#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generated="$(mktemp -d)"
trap 'rm -rf "${generated}"' EXIT

PLATFORM_CONSOLE_OUT_DIR="${generated}" pnpm --dir "${repo_root}/web" build

if ! diff --recursive --brief "${repo_root}/internal/consoleui/dist" "${generated}"; then
  echo "embedded console assets differ from a clean production build" >&2
  echo "run: pnpm --dir web build" >&2
  exit 1
fi

echo "embedded console assets match the production build"
