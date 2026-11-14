#!/usr/bin/env ruby
# frozen_string_literal: true

# The Rubix Kubernetes substrate CRDs under deploy/rubix-substrate/crds are
# ground truth: they are the CustomResourceDefinitions Palantir's compute
# substrate ships, recovered from com.palantir.rubixlib:rubix-lib-core, not
# authored here. This gate makes that provenance load-bearing. The manifest
# (crds-manifest.json) is the ledger; every claim it makes about a CRD -- the
# sha256 of the file, its group, kind, plural, short names, scope, served
# versions, and the top-level spec fields recovered from the tenant -- is
# checked against the CRD JSON itself. A CRD cannot be edited, and a manifest
# entry cannot be loosened, without the other being brought back into
# agreement, so drift away from the recovered schema fails here rather than
# shipping as a quiet re-authoring.
require "digest"
require_relative "lib/checked_json"

root = File.expand_path("..", __dir__)
substrate = File.join(root, "deploy/rubix-substrate")
manifest_path = File.join(substrate, "crds-manifest.json")
crds_dir = File.join(substrate, "crds")

manifest = JSON.parse(File.read(manifest_path))

abort "manifest schemaVersion must be pinned" unless manifest["schemaVersion"] == 1
entries = manifest["crds"]
abort "manifest lists no CRDs" unless entries.is_a?(Array) && !entries.empty?

# The manifest is the closed set: exactly the JSON files it names may sit in the
# directory, so a CRD added without a ledger entry -- or an orphaned file left
# after one was removed -- is caught, not silently carried.
on_disk = Dir.children(crds_dir).select { |name| name.end_with?(".json") }.sort
named = entries.map { |entry| entry["file"] }.sort
abort "CRD files on disk do not match the manifest\n  disk:     #{on_disk.join(', ')}\n  manifest: #{named.join(', ')}" unless on_disk == named
abort "manifest names a CRD file twice" unless named.uniq == named

seen_names = []
entries.each do |entry|
  file = entry["file"]
  path = File.join(crds_dir, file)
  abort "CRD file missing: #{file}" unless File.file?(path)

  bytes = File.binread(path)
  actual_sha = Digest::SHA256.hexdigest(bytes)
  abort "sha256 drift for #{file}\n  manifest: #{entry['sha256']}\n  actual:   #{actual_sha}" unless actual_sha == entry["sha256"]

  crd = JSON.parse(bytes)
  abort "#{file} is not an apiextensions/v1 object" unless crd["apiVersion"] == "apiextensions.k8s.io/v1"
  abort "#{file} is not a CustomResourceDefinition" unless crd["kind"] == "CustomResourceDefinition"

  spec = crd.fetch("spec")
  names = spec.fetch("names")
  metadata_name = crd.dig("metadata", "name")

  abort "#{file} group drift: #{spec['group']} != #{entry['group']}" unless spec["group"] == entry["group"]
  abort "#{file} kind drift: #{names['kind']} != #{entry['kind']}" unless names["kind"] == entry["kind"]
  abort "#{file} plural drift: #{names['plural']} != #{entry['plural']}" unless names["plural"] == entry["plural"]
  abort "#{file} scope drift: #{spec['scope']} != #{entry['scope']}" unless spec["scope"] == entry["scope"]

  # Kubernetes derives the resource name from plural + group; the manifest's
  # name field must be that exact join, and match the object's own metadata.
  expected_name = "#{entry['plural']}.#{entry['group']}"
  abort "#{file} manifest name #{entry['name'].inspect} is not #{expected_name.inspect}" unless entry["name"] == expected_name
  abort "#{file} metadata.name #{metadata_name.inspect} is not #{expected_name.inspect}" unless metadata_name == expected_name

  short_names = (names["shortNames"] || []).sort
  abort "#{file} shortNames drift: #{short_names} != #{entry['shortNames'].sort}" unless short_names == entry["shortNames"].sort

  served_versions = spec.fetch("versions").map { |version| version["name"] }.sort
  abort "#{file} version drift: #{served_versions} != #{entry['versions'].sort}" unless served_versions == entry["versions"].sort

  # Exactly one version stores, and every entry serves; a CRD that stopped
  # serving a version the manifest still lists would have drifted silently.
  storage_versions = spec["versions"].select { |version| version["storage"] }.map { |version| version["name"] }
  abort "#{file} must have exactly one storage version, has #{storage_versions.length}" unless storage_versions.length == 1
  abort "#{file} has a version that is not served" unless spec["versions"].all? { |version| version["served"] }

  # The spec fields the tenant recovery enumerated. This is the tightest tie to
  # ground truth: the recovered schema's top-level spec properties, on the
  # storage version, must be exactly the set the manifest records.
  storage = spec["versions"].find { |version| version["storage"] }
  properties = storage.dig("schema", "openAPIV3Schema", "properties", "spec", "properties") || {}
  actual_fields = properties.keys.sort
  expected_fields = entry.fetch("specFields").sort
  abort "#{file} spec field drift\n  manifest: #{expected_fields.join(', ')}\n  schema:   #{actual_fields.join(', ')}" unless actual_fields == expected_fields

  seen_names << metadata_name
end

abort "two CRDs claim the same resource name" unless seen_names.uniq == seen_names

# If a real cluster is on PATH, prove the recovered CRDs still register: a
# server-side dry-run applies each without persisting, so the same schema the
# manifest pins is the schema apiextensions accepts. Skipped when no cluster is
# reachable, which is the normal case in CI.
kubectl = ENV["KUBECTL_BIN"]
if kubectl && !kubectl.empty?
  entries.each do |entry|
    path = File.join(crds_dir, entry["file"])
    ok = system(kubectl, "apply", "--dry-run=server", "-f", path, out: File::NULL, err: File::NULL)
    abort "apiextensions rejected #{entry['file']} on server-side dry-run" unless ok
  end
  puts "Rubix substrate CRDs verified (#{entries.length} CRDs; server-side dry-run passed)"
else
  puts "Rubix substrate CRDs verified (#{entries.length} CRDs; set KUBECTL_BIN to also dry-run against a cluster)"
end
