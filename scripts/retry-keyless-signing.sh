#!/usr/bin/env bash
set -euo pipefail

if (( $# == 0 )); then
  echo "usage: retry-keyless-signing.sh COMMAND [ARG ...]" >&2
  exit 64
fi

max_attempts="${KEYLESS_SIGN_MAX_ATTEMPTS:-4}"
delay_seconds="${KEYLESS_SIGN_INITIAL_DELAY_SECONDS:-5}"

if [[ ! "${max_attempts}" =~ ^[1-9][0-9]*$ ]] || (( max_attempts > 10 )); then
  echo "KEYLESS_SIGN_MAX_ATTEMPTS must be an integer from 1 through 10" >&2
  exit 64
fi
if [[ ! "${delay_seconds}" =~ ^[0-9]+$ ]] || (( delay_seconds > 300 )); then
  echo "KEYLESS_SIGN_INITIAL_DELAY_SECONDS must be an integer from 0 through 300" >&2
  exit 64
fi

for ((attempt = 1; attempt <= max_attempts; attempt++)); do
  if "$@"; then
    exit 0
  else
    status=$?
  fi

  if (( attempt == max_attempts )); then
    echo "keyless signing command failed after ${attempt} attempts" >&2
    exit "${status}"
  fi

  echo "keyless signing command failed (attempt ${attempt}/${max_attempts}, exit ${status}); retrying in ${delay_seconds}s" >&2
  sleep "${delay_seconds}"
  delay_seconds=$((delay_seconds * 2))
done
