#!/usr/bin/env ruby
# frozen_string_literal: true

require "json"
require "open3"
require "rbconfig"
require "yaml"

reader = File.join(__dir__, "zarf-lifecycle-definition.rb")
image = "ghcr.io/example/hub@sha256:#{'a' * 64}"
base = {"apiVersion" => "zarf.dev/v1alpha1", "kind" => "ZarfPackageConfig",
        "metadata" => {"name" => "platform-runtime", "version" => "0.1.0"},
        "constants" => [{"name" => "HUB_IMAGE", "value" => image}]}
cases = 0
check = lambda do |text, field, expected, reason = nil|
  out, err, status = Open3.capture3(RbConfig.ruby, reader, field, "0.1.0", stdin_data: text)
  if reason
    abort "definition refusal lost its cause: #{out}#{err}" if status.success? || !err.include?(reason)
  else
    abort "wrong lifecycle field #{field}: #{out}#{err}" unless status.success? && out.strip == expected
  end
  cases += 1
end

%w[zarf.dev/v1alpha1 zarf.dev/v1beta1].each do |api|
  definition = Marshal.load(Marshal.dump(base))
  definition["apiVersion"] = api
  if api.end_with?("v1beta1")
    definition.delete("constants")
    definition["metadata"]["annotations"] = {"conduit.dev/hub-image" => image}
  end
  check.call(YAML.dump(definition), "api", api)
  check.call(YAML.dump(definition), "version", "0.1.0")
  check.call(YAML.dump(definition), "hub", image)
  definition["metadata"]["version"] = "0.2.0"
  check.call(YAML.dump(definition), "api", nil, "unexpected package version")
end
{
  "unsupported package API" => ->(d) { d["apiVersion"] = "zarf.dev/v2" },
  "unexpected package identity" => ->(d) { d["metadata"]["name"] = "other" },
  "ambiguous Hub image binding" => ->(d) { d["constants"] *= 2 },
  "package has no immutable Hub image binding" => ->(d) { d["constants"][0]["value"] = "ghcr.io/example/hub:latest" }
}.each do |reason, mutate|
  definition = Marshal.load(Marshal.dump(base))
  mutate.call(definition)
  check.call(YAML.dump(definition), "hub", nil, reason)
end
check.call(YAML.dump(base) + "---\n{}\n", "api", nil, "expected one package definition")
check.call(" " * ((16 << 20) + 1), "api", nil, "package definition exceeds the size limit")
puts "Lifecycle definition reader passes #{cases} alpha/beta identity, version, image and refusal cases"
