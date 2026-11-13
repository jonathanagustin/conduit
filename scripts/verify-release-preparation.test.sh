#!/usr/bin/env bash
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT
ruby "$root/scripts/verify-release-preparation.rb"
# Every mutation parses as valid YAML. The contract must refuse semantics,
# rather than getting a green score by rejecting malformed files.
for mutation in tag_creation publication_job unpinned_action extra_permission unbounded_runtime foreign_branch; do
  ruby -ryaml -e '
    document = YAML.safe_load_file(ARGV[0], aliases: true)
    job = document["jobs"]["prepare"]
    case ARGV[2]
    when "tag_creation" then job["steps"][3]["with"]["skip-github-release"] = false
    when "publication_job" then document["jobs"]["publish"] = {"uses" => "./.github/workflows/platform-images.yml"}
    when "unpinned_action" then job["steps"][3]["uses"] = "googleapis/release-please-action@v5"
    when "extra_permission" then job["permissions"]["packages"] = "write"
    when "unbounded_runtime" then job.delete("timeout-minutes")
    when "foreign_branch" then job.delete("if")
    end
    File.write(ARGV[1], YAML.dump(document))
  ' "$root/.github/workflows/release-please.yml" "$fixture/$mutation.yml" "$mutation"
  if ruby "$root/scripts/verify-release-preparation.rb" "$fixture/$mutation.yml" > "$fixture/$mutation.log" 2>&1; then
    echo "release preparation accepted mutation $mutation" >&2
    exit 1
  fi
  echo "refused $mutation"
done
