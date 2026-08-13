#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
module_directory="${repo_root}/examples/modules"

ruby "${repo_root}/docs/apollo/validate-module-definitions.rb"

if [[ -z "${APOLLO_CLI:-}" ]]; then
  echo "Local Module validation passed; APOLLO_CLI is unset, so native lock verification was skipped."
  exit 0
fi
if [[ ! -x "${APOLLO_CLI}" ]]; then
  echo "APOLLO_CLI must name an executable tenant-downloaded Apollo CLI" >&2
  exit 2
fi

"${APOLLO_CLI}" module lock --verify --module-directory "${module_directory}"
