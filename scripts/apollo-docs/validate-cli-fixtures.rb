#!/usr/bin/env ruby
# frozen_string_literal: true

require_relative "../lib/checked_json"
require "pathname"
require "yaml"

ROOT = Pathname.new(__dir__).join("../..").expand_path
FIXTURES = ROOT.join("docs/reference-platform/cli-fixtures")
RECONSTRUCTED = FIXTURES.join("reconstructed/0.814.0")
MANIFEST_KEYS = %w[manifest-version product-group product-name product-version product-type extensions].freeze
HELM_KEYS = %w[helm-chart-name helm-chart-version helm-repository-url].freeze
ORDERABLE_VERSION = /\A(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)(?:(?:-rc(?:0|[1-9]\d*))?(?:-(?:0|[1-9]\d*)-g[0-9a-f]+)?)?\z/

def abort_unless(condition, message)
  abort message unless condition
end

def yaml(path)
  YAML.safe_load(path.read, permitted_classes: [], aliases: false)
rescue Psych::Exception => e
  abort "invalid YAML #{path}: #{e.message}"
end

def validate_orderable_version(value, path)
  text = value.to_s
  abort_unless(ORDERABLE_VERSION.match?(text), "#{path} is not an Apollo orderable version: #{text.inspect}")
  core = text.split("-", 2).first.split(".").map(&:to_i)
  abort_unless(core.all? { |part| part <= 2_147_483_647 }, "#{path} exceeds Apollo's signed 32-bit component limit")
end

def validate_manifest(manifest, source)
  abort_unless(manifest.is_a?(Hash), "#{source} must contain a YAML mapping")
  missing = MANIFEST_KEYS - manifest.keys
  abort_unless(missing.empty?, "#{source} is missing keys: #{missing.join(', ')}")
  abort_unless(manifest["manifest-version"].to_s == "1.0", "#{source} manifest-version must be 1.0")
  abort_unless(manifest["product-type"] == "helm-chart.v1", "#{source} must use product-type helm-chart.v1")
  abort_unless(manifest["product-group"].is_a?(String) && !manifest["product-group"].empty?, "#{source} product-group is required")
  abort_unless(manifest["product-name"].is_a?(String) && !manifest["product-name"].empty?, "#{source} product-name is required")
  validate_orderable_version(manifest["product-version"], "#{source} product-version")

  extensions = manifest["extensions"]
  abort_unless(extensions.is_a?(Hash), "#{source} extensions must be a mapping")
  helm = extensions["helm-chart"]
  abort_unless(helm.is_a?(Hash), "#{source} extensions.helm-chart must be a mapping")
  missing_helm = HELM_KEYS - helm.keys
  abort_unless(missing_helm.empty?, "#{source} helm-chart extension is missing: #{missing_helm.join(', ')}")
  validate_orderable_version(helm["helm-chart-version"], "#{source} helm-chart-version")
  repository = helm["helm-repository-url"]
  abort_unless(repository.is_a?(String) && repository.match?(%r{\A(?:https|oci)://}), "#{source} repository must use https:// or oci://")

  artifacts = extensions.fetch("artifacts", [])
  abort_unless(artifacts.is_a?(Array), "#{source} artifacts extension must be a list")
  artifacts.each_with_index do |artifact, index|
    abort_unless(artifact.is_a?(Hash) && artifact["type"] == "oci", "#{source} artifact #{index} must have type oci")
    abort_unless(artifact["uri"].is_a?(String) && !artifact["uri"].empty?, "#{source} artifact #{index} URI is required")
  end
end

manifest_path = RECONSTRUCTED.join("deployment/manifest.yml")
expected_manifest = yaml(manifest_path)
validate_manifest(expected_manifest, manifest_path)

input_values = yaml(FIXTURES.join("input-chart/values.yaml"))
archive_values = yaml(RECONSTRUCTED.join("deployment/values.yaml"))
abort_unless(input_values == archive_values, "reconstructed archive values.yaml differs from the input chart")

input_schema = JSON.parse(FIXTURES.join("input-chart/values.schema.json").read)
archive_schema = JSON.parse(RECONSTRUCTED.join("deployment/values.schema.json").read)
abort_unless(input_schema == archive_schema, "reconstructed archive values.schema.json differs from the input chart")

expected_files = RECONSTRUCTED.join("archive-files.txt").read.lines.map(&:strip).reject(&:empty?).sort
required_suffixes = %w[deployment/manifest.yml deployment/values.yaml deployment/values.schema.json]
required_suffixes.each do |suffix|
  abort_unless(expected_files.one? { |entry| entry.end_with?(suffix) }, "reconstructed archive is missing #{suffix}")
end

captured = FIXTURES.join("captured")
capture_count = 0
if captured.directory?
  captured.children.select(&:directory?).sort.each do |capture|
    generated = capture.join("generated")
    next unless generated.directory?

    captured_manifest_path = generated.join("archive-manifest.yml")
    captured_manifest_path = generated.join("manifest.yml") unless captured_manifest_path.file?
    next unless captured_manifest_path.file?

    captured_manifest = yaml(captured_manifest_path)
    validate_manifest(captured_manifest, captured_manifest_path)
    abort_unless(captured_manifest == expected_manifest,
      "#{captured_manifest_path} differs semantically from the reconstructed 0.814.0 manifest")

    inventory = generated.join("archive-files.txt")
    if inventory.file?
      files = inventory.read.lines.map(&:strip).reject(&:empty?)
      required_suffixes.each do |suffix|
        abort_unless(files.one? { |entry| entry.end_with?(suffix) }, "#{inventory} is missing #{suffix}")
      end
    end
    capture_count += 1
  end
end

puts "Apollo CLI fixtures validated: reconstructed 0.814.0 contract, #{capture_count} native capture(s)"
