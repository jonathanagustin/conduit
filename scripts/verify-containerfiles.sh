#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
hadolint_bin="${HADOLINT_BIN:-hadolint}"

"${hadolint_bin}" --failure-threshold info \
  "${repo_root}/Containerfile" \
  "${repo_root}/Containerfile.agent"
