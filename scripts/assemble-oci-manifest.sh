#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -lt 3 ]]; then
  echo "usage: $0 OUTPUT HUB_DESCRIPTOR SPOKE_DESCRIPTOR" >&2
  exit 2
fi

output="$1"
shift

jq --exit-status --slurp '
  length == 2 and
  ([.[].component] | sort == ["hub", "spoke-agent"]) and
  ([.[].version] | unique | length == 1) and
  ([.[].commit] | unique | length == 1) and
  all(.[]; .digest | test("^sha256:[0-9a-f]{64}$")) and
  all(.[]; [.sboms[].architecture] | sort == ["amd64", "arm64"]) and
  all(.[]; [.vulnerabilityReports[].architecture] | sort == ["amd64", "arm64"]) and
  all(.[]; all(.sboms[]; .sha256 | test("^[0-9a-f]{64}$"))) and
  all(.[]; all(.vulnerabilityReports[]; .sha256 | test("^[0-9a-f]{64}$"))) and
  all(.[]; all(.sboms[]; . as $record | $record.file | endswith("-" + $record.architecture + ".spdx.json"))) and
  all(.[]; all(.vulnerabilityReports[]; . as $record | $record.file | endswith("-" + $record.architecture + ".grype.json"))) and
  all(.[]; .reference == (.image + "@" + .digest))
' "$@" >/dev/null

jq --slurp '
  sort_by(.component) as $items |
  {
    apiVersion: "platform.delivery/oci-release/v2",
    version: $items[0].version,
    commit: $items[0].commit,
    images: ($items | map({key: .component, value: {image, digest, reference, sboms, vulnerabilityReports}}) | from_entries)
  }
' "$@" >"${output}"
