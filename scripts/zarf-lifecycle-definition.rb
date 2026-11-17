#!/usr/bin/env ruby
# frozen_string_literal: true

require_relative "lib/safe_yaml"

# The caller authenticates archive definitions before using this reader. A
# deployed definition is observation only; it supplies no signature authority.
text = STDIN.read((16 << 20) + 1)
abort "package definition exceeds the size limit" if text.bytesize > (16 << 20)
documents = ConduitSafeYAML.stream(text)
abort "expected one package definition" unless documents.length == 1 && documents.first.is_a?(Hash)
definition = documents.first
api = definition.fetch("apiVersion")
abort "unsupported package API" unless %w[zarf.dev/v1alpha1 zarf.dev/v1beta1].include?(api)
abort "unexpected package identity" unless definition["kind"] == "ZarfPackageConfig" && definition.dig("metadata", "name") == "platform-runtime"
version = definition.dig("metadata", "version").to_s
abort "unexpected package version" if ARGV[1] && version != ARGV[1]

case ARGV.fetch(0)
when "api" then puts api
when "version" then puts version
when "hub"
  image = if api == "zarf.dev/v1beta1"
    definition.dig("metadata", "annotations", "conduit.dev/hub-image")
  else
    matches = definition.fetch("constants").select { |item| item["name"] == "HUB_IMAGE" }
    abort "ambiguous Hub image binding" unless matches.length == 1
    matches.first.fetch("value")
  end
  abort "package has no immutable Hub image binding" unless image.is_a?(String) && image.match?(/\A[A-Za-z0-9._:\/-]+@sha256:[a-f0-9]{64}\z/)
  puts image
else abort "unknown package definition field"
end
