#!/usr/bin/env ruby
# frozen_string_literal: true
require "yaml"
require "json"
root = File.expand_path("..", __dir__)
workflow_path = ARGV.fetch(0, File.join(root, ".github/workflows/release-please.yml"))
config_path = ARGV.fetch(1, File.join(root, "release-please-config.json"))
workflow = YAML.safe_load_file(workflow_path, aliases: true)
config = JSON.parse(File.read(config_path))
check = lambda { |condition, message| abort "release preparation: #{message}" unless condition }
triggers = workflow["on"] || workflow[true]
check.call(triggers.keys.sort == %w[push workflow_dispatch], "only main pushes and manual preparation are permitted")
check.call(triggers.dig("push", "branches") == ["main"], "preparation must target only main")
check.call(workflow["permissions"] == {"contents" => "read"}, "default permissions must be read-only")
check.call(workflow.dig("concurrency", "cancel-in-progress") == true, "superseded preparation must cancel")
check.call(workflow["jobs"].keys == ["prepare"], "preparation may not add publication jobs")
job = workflow.dig("jobs", "prepare")
check.call(job["if"] == "github.ref == 'refs/heads/main'", "manual dispatch must also target main")
check.call(job["timeout-minutes"] == 10, "preparation must cap runtime")
check.call(job["permissions"] == {"contents" => "write", "pull-requests" => "write", "actions" => "write"}, "preparation permissions must match the reviewed PR and Trunk handoff")
steps = job["steps"]
check.call(steps.length == 5, "preparation must have only its reviewed five steps")
check.call(steps[0]["uses"] == "actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803" && steps[0].dig("with", "persist-credentials") == false, "checkout must be pinned without persisted credentials")
check.call(steps[1]["uses"] == "actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1", "Node setup must be pinned")
check.call(steps[2]["run"] == "node scripts/release-preparation.mjs reconcile" && steps[4]["run"] == "node scripts/release-preparation.mjs verify", "only publication reconciliation and Trunk handoff may run")
check.call(steps[3]["uses"] == "googleapis/release-please-action@45996ed1f6d02564a971a2fa1b5860e934307cf7", "Release Please must use the reviewed full SHA")
check.call(steps[3]["with"] == {"token" => "${{ github.token }}", "target-branch" => "main", "config-file" => "release-please-config.json", "manifest-file" => ".release-please-manifest.json", "skip-github-release" => true}, "Release Please must remain PR-only with the built-in token")
check.call([steps[2], steps[4]].all? { |step| step["env"] == {"GH_TOKEN" => "${{ github.token }}"} }, "helper authority must use only the built-in token")
check.call(config["packages"].keys == ["."], "Conduit has one product version")
package = config["packages"]["."]
check.call(package["release-type"] == "simple" && package["version-file"] == "VERSION", "VERSION must stay authoritative")
check.call(config["include-component-in-tag"] == false && config["include-v-in-tag"] == true, "tags must stay vX.Y.Z")
check.call(config["bootstrap-sha"] == "92e0c6f6258b68f7bebf5da04978ac54ec337844", "bootstrap must remain the published v0.17.5 commit")
check.call(package["bump-minor-pre-major"] == true && package["bump-patch-for-minor-pre-major"] == false, "pre-1.0 semantics changed without a policy decision")
expected = %w[platform-hub platform-spoke].flat_map { |chart| %w[version appVersion].map { |key| {"type" => "yaml", "path" => "deploy/helm/#{chart}/Chart.yaml", "jsonpath" => "$.#{key}"} } }
check.call(package["extra-files"] == expected, "version updates must enumerate the two charts only")
check.call(config["pull-request-title-pattern"] == 'chore(main): release ${version}', "release titles must use Conventional Commits")
check.call(!config["pull-request-header"].to_s.empty? && !config["pull-request-footer"].to_s.empty?, "custom metadata must suppress upstream signature defaults")
check.call(![config["pull-request-header"],config["pull-request-footer"]].join.match?(/generated with|beep|co-authored-by|openai|claude|codex/i), "release metadata must contain no attribution")
version = File.read(File.join(root, "VERSION")).strip
check.call(JSON.parse(File.read(File.join(root, ".release-please-manifest.json"))) == {"." => version}, "manifest must agree with VERSION")
%w[platform-hub platform-spoke].each do |chart|
  metadata = YAML.safe_load_file(File.join(root, "deploy/helm", chart, "Chart.yaml"))
  check.call(metadata["version"] == version && metadata["appVersion"] == version, "chart versions must agree with VERSION")
end
puts "release preparation: PR-only, explicit versions, bounded Trunk dispatch, manual publication preserved"
