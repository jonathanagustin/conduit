#!/usr/bin/env ruby
# frozen_string_literal: true

require "yaml"

def fail_gate(message)
  warn "verify-optimizer-candidate-release-workflow: #{message}"
  exit 1
end

repository_root = `git rev-parse --show-toplevel`.strip
fail_gate("not running in a Git checkout") unless $?.success? && !repository_root.empty?

workflow = ARGV.fetch(0, File.join(repository_root, ".github/workflows/platform-optimizer-candidate-release.yml"))
document = YAML.safe_load_file(workflow, aliases: true)
fail_gate("workflow document must be a mapping") unless document.is_a?(Hash)

# Psych implements YAML 1.1 and decodes an unquoted GitHub `on` key as true.
# Accept that parser representation, but require exactly one manual trigger.
triggers = document["on"] || document[true]
fail_gate("workflow must have exactly one workflow_dispatch trigger") unless
  triggers.is_a?(Hash) && triggers.keys == ["workflow_dispatch"]

inputs = triggers.dig("workflow_dispatch", "inputs")
fail_gate("backend input is not the closed HiGHS/CP-SAT choice") unless
  inputs.dig("backend", "required") == true &&
  inputs.dig("backend", "options") == %w[highs cpsat]
%w[revision authorization].each do |name|
  fail_gate("#{name} input must be required") unless inputs.dig(name, "required") == true
end

fail_gate("top-level permissions must remain read-only") unless document["permissions"] == {"contents" => "read"}
fail_gate("publication runs must never be cancelled") unless document.dig("concurrency", "cancel-in-progress") == false

jobs = document["jobs"]
fail_gate("workflow jobs are absent") unless jobs.is_a?(Hash)
publish_name = "publish-unadmitted-candidate"
publish = jobs[publish_name]
fail_gate("publish job is absent") unless publish.is_a?(Hash)
fail_gate("publish job must use the protected optimizer environment") unless
  publish["environment"] == "optimizer-candidate-publish"
fail_gate("publish job must depend on the cross-architecture gate") unless
  Array(publish["needs"]) == ["cross-architecture-gate"]
fail_gate("publish job permissions are not the closed release set") unless
  publish["permissions"] == {"contents" => "read", "id-token" => "write", "packages" => "write"}

jobs.each do |job_name, job|
  permissions = job.fetch("permissions", {})
  if job_name != publish_name && permissions.values.any? { |value| value == "write" }
    fail_gate("non-publish job #{job_name} has write permission")
  end

  Array(job["steps"]).each do |step|
    uses = step["uses"]
    if uses && !uses.start_with?("./") && !uses.match?(/@[0-9a-f]{40}\z/)
      fail_gate("third-party Action is not pinned to a full commit SHA: #{uses}")
    end
    if uses&.start_with?("actions/checkout@") && step.dig("with", "ref") != "${{ inputs.revision }}"
      fail_gate("checkout in #{job_name} is not bound to inputs.revision")
    end
    next if job_name == publish_name

    run = step["run"].to_s
    if run.match?(/\bdocker push\b|\bcosign (?:sign|attest)\b|\bimagetools create\b/)
      fail_gate("external publication command exists outside the protected publish job")
    end
  end
end

publish_steps = Array(publish["steps"])
authorization_index = publish_steps.index { |step| step["name"] == "Revalidate explicit protected-environment authorization" }
login_index = publish_steps.index { |step| step["uses"].to_s.start_with?("docker/login-action@") }
fail_gate("authorization must run before registry login") unless
  authorization_index && login_index && authorization_index < login_index

all_run = jobs.values.flat_map { |job| Array(job["steps"]).map { |step| step["run"].to_s } }.join("\n")
publish_run = publish_steps.map { |step| step["run"].to_s }.join("\n")
required_fragments = [
  '[[ "${REVISION}" == "${GITHUB_SHA}" ]]',
  '"publish:${BACKEND}:${REVISION}"',
  'OPTIMIZER_CANDIDATE_PUBLISH_POLICY=manual-v1',
  "assemble-optimizer-candidate-gate.sh",
  "docker builder prune --all --force",
  "--no-cache",
  "highs) source_date_epoch=1786406400",
  "cpsat) source_date_epoch=1768232310",
  '--build-arg "SOURCE_DATE_EPOCH=${source_date_epoch}"',
  "byteIdenticalImage",
  'cmp "${gate}" "${TMPDIR}/revalidated-candidate-gate.json"',
  'admission: "UNADMITTED"',
  "selectable: false",
  "registryMutated: true",
  "optimizerStateMutated: false",
  "cosign verify-attestation"
]
required_fragments.each do |fragment|
  fail_gate("required fail-closed contract is absent: #{fragment}") unless all_run.include?(fragment)
end
fail_gate("both native builds must pass the pinned source epoch explicitly") unless
  all_run.scan('--build-arg "SOURCE_DATE_EPOCH=${source_date_epoch}"').length == 2

fail_gate("publish job does not push immutable digest-addressed manifests") unless
  publish_run.include?('platform_reference[$architecture]="${image}@${digest}"') &&
  publish_run.include?('index_reference="${image}@${index_digest}"')
fail_gate("workflow must not write optimizer admission or lifecycle state") if
  all_run.match?(/record-capability-(?:admission|lifecycle)|OptimizerCapabilityLifecycleStore/)

puts "optimizer candidate release workflow: pass"
