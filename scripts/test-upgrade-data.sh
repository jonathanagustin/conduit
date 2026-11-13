#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
command -v go >/dev/null
command -v docker >/dev/null
command -v python3 >/dev/null
docker info >/dev/null
cd "${root}"
export PYTHONDONTWRITEBYTECODE=1
python3 -m unittest discover -s tools/upgrade-data -p '*_test.py'
exec python3 tools/upgrade-data/rehearse.py --receipt "${1:?usage: test-upgrade-data.sh receipt.json}"
