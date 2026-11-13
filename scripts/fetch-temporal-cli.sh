#!/usr/bin/env bash
# Download the pinned Temporal CLI used by the packaging proof. The binary is
# cached under .cache and is not a repository artifact. Re-running checks the
# digest and does not download again.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
url="https://github.com/temporalio/cli/releases/download/v1.10.0/temporal_cli_1.10.0_linux_amd64.tar.gz"
sha="521f461f3d108902763bb71e85cd0fd382dc9357b59604ae03da6ae734d16890"
dest="${root}/.cache/temporal"
archive="${dest}/temporal_cli_1.10.0_linux_amd64.tar.gz"
mkdir -p "${dest}"
if [[ ! -f "${archive}" ]]; then
  curl -fsSL -o "${archive}" "${url}"
fi
echo "${sha}  ${archive}" | sha256sum -c -
if [[ ! -x "${dest}/temporal" ]]; then
  tar -xzf "${archive}" -C "${dest}"
fi
"${dest}/temporal" --version
