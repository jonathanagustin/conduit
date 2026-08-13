#!/usr/bin/env bash
set -euo pipefail

body="$(cat)"

require_section_content() {
  local heading="$1"
  awk -v heading="$heading" '
    $0 == heading { inside = 1; next }
    inside && /^##/ { exit }
    inside && $0 !~ /^[[:space:]]*$/ && $0 !~ /^[[:space:]]*<!--/ { found = 1 }
    END { exit(found ? 0 : 1) }
  ' <<<"$body" || {
    printf 'pull request section %s is missing or empty\n' "$heading" >&2
    exit 1
  }
}

require_section_content '## Outcome'
require_section_content '### Deliberate divergences'

if grep -Eiq '\b(TBD|TODO|PLACEHOLDER|FILL[ -]?ME)\b' <<<"$body"; then
  echo 'pull request body still contains a placeholder' >&2
  exit 1
fi

valid_rows="$({
  awk -F '|' '
    function trim(value) {
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      return value
    }
    /^\|/ {
      claim = trim($2)
      class = trim($3)
      source = trim($4)
      scope = trim($5)
      if (class == "Public contract" || class == "Tenant observation" ||
          class == "Corroborating" || class == "Corroborating evidence" ||
          class == "Conduit extension" || class == "Unknown") {
        if (claim == "" || source == "" || scope == "") {
          print "incomplete:" NR
        } else {
          print "valid:" NR
        }
      }
    }
  ' <<<"$body"
} || true)"

if grep -q '^incomplete:' <<<"$valid_rows"; then
  echo 'every tenant-fidelity row needs a claim, exact class, source, and unsupported scope' >&2
  exit 1
fi
if ! grep -q '^valid:' <<<"$valid_rows"; then
  echo 'pull request has no completed tenant-fidelity claim row' >&2
  exit 1
fi

echo 'tenant-fidelity pull-request record is complete'
