#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FF_DOCKER_ACR_STRICT_HOST_CHECK=true go -C "$root/tools/cosign-build" test -count=1 github.com/chrismellard/docker-credential-acr-env/pkg/credhelper
