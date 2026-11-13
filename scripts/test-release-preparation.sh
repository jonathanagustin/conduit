#!/usr/bin/env bash
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
cd "$root"
./scripts/verify-release-preparation.test.sh
npm --prefix tools/release-please ci --ignore-scripts --no-audit --no-fund
npm --prefix tools/release-please test
