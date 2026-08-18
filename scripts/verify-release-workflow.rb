#!/usr/bin/env ruby
# frozen_string_literal: true

# The shape of the release path, asserted rather than described.
#
# Two properties matter and neither is visible from reading one file:
#
#   1. Releasing is ONE human instruction and never fewer. platform-release.yml
#      may have exactly one trigger, workflow_dispatch, and so may the
#      publication workflow it hands the tag to. ADR 0001 keeps publication
#      manual because hosted Actions time is controlled, and A5 says a green
#      gate never authorizes a release; an automatic trigger on either file
#      would overturn both silently.
#   2. Publication cannot start from a non-tag ref. platform-images.yml gates
#      its charts, runtime source, air-gap packages and CLI on the ref being a
#      tag, and does not gate the image push, so a branch dispatch published a
#      fraction of a release and reported success (#196). The preflight job that
#      closes that is only a guard while every other job still needs it, which
#      is a property of the graph, not of any single job.
#
# Kept in its own file rather than added to verify-trusted-supply-chain.rb: that
# file asserts what the published artifacts are, this one asserts who may start
# a publication and from where.
require "yaml"

def fail_gate(message)
  warn "verify-release-workflow: #{message}"
  exit 1
end

repository_root = `git rev-parse --show-toplevel`.strip
fail_gate("not running in a Git checkout") unless $?.success? && !repository_root.empty?

workflow_dir = File.join(repository_root, ".github/workflows")
release_path = ARGV.fetch(0, File.join(workflow_dir, "platform-release.yml"))
images_path = ARGV.fetch(1, File.join(workflow_dir, "platform-images.yml"))
ci_path = ARGV.fetch(2, File.join(workflow_dir, "platform-ci.yml"))

release = YAML.safe_load_file(release_path, aliases: true)
images = YAML.safe_load_file(images_path, aliases: true)
fail_gate("release workflow must be a mapping") unless release.is_a?(Hash)
fail_gate("publication workflow must be a mapping") unless images.is_a?(Hash)

# Psych implements YAML 1.1 and decodes an unquoted GitHub `on` key as true, so
# the trigger block is read under whichever key the parser produced.
triggers = lambda do |document|
  document["on"] || document[true]
end

fail_gate("release workflow must have exactly one workflow_dispatch trigger") unless
  triggers.call(release).is_a?(Hash) && triggers.call(release).keys == ["workflow_dispatch"]
fail_gate("publication workflow must have exactly one workflow_dispatch trigger") unless
  triggers.call(images).is_a?(Hash) && triggers.call(images).keys == ["workflow_dispatch"]

release_inputs = triggers.call(release).dig("workflow_dispatch", "inputs")
fail_gate("release must take a required version input") unless release_inputs.dig("version", "required") == true

fail_gate("release workflow's top-level permissions must remain read-only") unless
  release["permissions"] == {"contents" => "read"}
# A cancelled release can leave a tag with nothing published against it, which
# is the half-finished state the workflow exists to remove.
fail_gate("a release run must never be cancelled by a newer one") unless
  release.dig("concurrency", "cancel-in-progress") == false

release_jobs = release["jobs"]
fail_gate("release workflow declares no jobs") unless release_jobs.is_a?(Hash)
release_job = release_jobs["release"]
fail_gate("release workflow must declare a release job") unless release_job.is_a?(Hash)
fail_gate("release job holds authority it does not use, or lacks authority it does") unless
  release_job["permissions"] == {"actions" => "write", "contents" => "write", "statuses" => "read"}
fail_gate("release job must cap its runtime") unless release_job["timeout-minutes"].is_a?(Integer)

release_steps = Array(release_job["steps"])
release_steps.each do |step|
  uses = step["uses"]
  next unless uses && !uses.start_with?("./")

  fail_gate("third-party Action is not pinned to a full commit SHA: #{uses}") unless uses.match?(/@[0-9a-f]{40}\z/)
end

step_index = lambda do |pattern|
  release_steps.index { |step| step["run"].to_s.match?(pattern) }
end
preconditions_index = step_index.call(%r{scripts/verify-release-preconditions\.sh})
tag_index = step_index.call(%r{gh api --method POST[\s\\]+"repos/\$\{GITHUB_REPOSITORY\}/git/refs"})
dispatch_index = step_index.call(/gh workflow run platform-images\.yml/)

fail_gate("release must verify its preconditions") unless preconditions_index
fail_gate("release must create the tag itself") unless tag_index
fail_gate("release must dispatch publication") unless dispatch_index
# Order is the guarantee. Preconditions checked after the tag exists is a
# refusal that arrives too late to refuse anything: a tag is immutable, so the
# repair for a wrong one is a version nobody wanted to burn.
fail_gate("preconditions must be verified before the tag is created") unless preconditions_index < tag_index
fail_gate("the tag must exist before publication is dispatched against it") unless tag_index < dispatch_index

release_run = release_steps.map { |step| step["run"].to_s }.join("\n")
fail_gate("the created tag must be read back and required to name the released commit") unless
  release_run.include?('test "${created}" = "${GITHUB_SHA}"')
fail_gate("publication must be dispatched against the tag, not a branch") unless
  release_run.include?('--repo "${GITHUB_REPOSITORY}" --ref "${tag}"')
# Signing, publishing and releasing belong to platform-images.yml, which is the
# only workflow holding registry authority. A release job that grew one of them
# would be a second publication path with none of that file's gates.
fail_gate("the release job must not publish anything itself") if
  release_run.match?(/\bcosign \b|\bhelm push\b|\bdocker push\b|\bgh release (?:create|upload)\b/)

images_jobs = images["jobs"]
fail_gate("publication workflow declares no jobs") unless images_jobs.is_a?(Hash)
preflight_name = "verify-publication-ref"
preflight = images_jobs[preflight_name]
fail_gate("publication workflow must declare a #{preflight_name} job") unless preflight.is_a?(Hash)
preflight_run = Array(preflight["steps"]).map { |step| step["run"].to_s }.join("\n")
fail_gate("#{preflight_name} must run the ref guard") unless preflight_run.include?("./scripts/verify-publication-ref.sh")

# The guard only guards while everything depends on it, directly or through the
# chain. A job that quietly dropped the edge would publish from a branch again,
# and the run would be green.
reaches_preflight = lambda do |name, seen|
  return false if seen.include?(name)

  needs = Array(images_jobs.dig(name, "needs"))
  needs.include?(preflight_name) || needs.any? { |dependency| reaches_preflight.call(dependency, seen + [name]) }
end
images_jobs.each_key do |name|
  next if name == preflight_name

  fail_gate("publication job #{name} does not depend on #{preflight_name}") unless reaches_preflight.call(name, [])
end

%w[verify-release-preconditions.sh verify-publication-ref.sh].each do |script|
  path = File.join(repository_root, "scripts", script)
  fail_gate("#{script} must exist and be executable") unless File.executable?(path)
end

# The precondition script reads a commit status by an exact context string, and
# platform-ci.yml writes one. A rename on either side alone would not fail
# anything: the release would simply find no status, which reads as "no gate has
# run" rather than "the gate failed", and the two are not the same refusal.
preconditions = File.read(File.join(repository_root, "scripts/verify-release-preconditions.sh"), encoding: "UTF-8")
context = preconditions[/^trunk_gate_context='([^']+)'/, 1]
fail_gate("verify-release-preconditions.sh names no Trunk gate status context") if context.nil?
ci = File.read(ci_path, encoding: "UTF-8")
fail_gate("the status context the release requires (#{context}) is not the one platform-ci.yml publishes") unless
  ci.include?("-f context='#{context}'")

puts "release path: one dispatch, verified preconditions, tag, then publication from the tag"
