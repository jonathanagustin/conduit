#!/usr/bin/env bash
# Governed Excalidraw packaging. PACKAGING_REPO_URL defaults to the pinned source.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
export PACKAGING_REPO_URL="${PACKAGING_REPO_URL:-https://github.com/excalidraw/excalidraw}"
export PACKAGING_REF="${PACKAGING_REF:-a2ec2889babf7d2295469c6d90ebe77fae57df84}"
export CONDUIT_PACKAGING_BUILD="${CONDUIT_PACKAGING_BUILD:-source}"
exec ./scripts/governed-packaging-run.sh
