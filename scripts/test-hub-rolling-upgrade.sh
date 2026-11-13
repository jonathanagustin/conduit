#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
[[ "$(uname -s)" == "Linux" ]] || { echo "this image fixture requires a Linux host" >&2; exit 1; }
for tool in go docker python3 helm kubectl openssl tar git; do
  command -v "${tool}" >/dev/null || { echo "${tool} is required" >&2; exit 1; }
done
docker info >/dev/null
cd "${root}"
export PYTHONDONTWRITEBYTECODE=1
python3 -m unittest discover -s tools/upgrade-rolling -p '*_test.py'
exec python3 tools/upgrade-rolling/rolling.py --receipt "${1:?usage: test-hub-rolling-upgrade.sh receipt.json}"
