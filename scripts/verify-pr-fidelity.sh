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

# An unfilled template marker means the body was started and not finished, and
# merging one publishes a description nobody wrote.
#
# Two things this deliberately does NOT do, both learned by rejecting correct
# work twice in a row on one pull request:
#
#   - It matches case-sensitively. These are markers, and a marker copied out of
#     a template is upper case. Matching case-insensitively also caught the
#     ordinary English words, so a body describing an API example -- "the
#     stand-in value 123" -- was unpublishable, and the author had to discover by
#     failing CI which words the gate had reserved.
#   - It ignores code spans and fenced blocks. A body explaining a template, or
#     quoting this list, has to be able to write the markers down. The cost is
#     that a marker hidden in backticks passes; the benefit is that documenting
#     the mechanism is possible at all, and an unfilled marker is not in
#     backticks, because nobody formats the thing they forgot to replace.
#
# Only this check reads the stripped text. The section checks above read the
# body as written, because a section whose only content is a code block is
# content.
prose="$(awk '
  /^[[:space:]]*```/ { fenced = !fenced; next }
  !fenced { print }
' <<<"$body" | sed 's/`[^`]*`//g')"

if grep -Eq '\b(TBD|TODO|PLACEHOLDER|FILL[ -]?ME)\b' <<<"$prose"; then
  echo 'pull request body still contains an unfilled template marker (TBD, TODO, PLACEHOLDER or FILL ME, upper case, outside code)' >&2
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
