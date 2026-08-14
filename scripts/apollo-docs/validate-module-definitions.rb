#!/usr/bin/env ruby
# frozen_string_literal: true

require "pathname"
require "yaml"

root = Pathname.new(__dir__).join("../..").expand_path
# Defaults to this tree's conformance examples. Product repositories pass their
# own Module directory so this same contract gates what their tenants pull,
# without making application catalogs part of Conduit's repository.
directory = ARGV[0] ? Pathname.new(ARGV[0]).expand_path : root.join("examples/modules")
files = directory.glob("*.yaml").sort
abort "no Apollo Module definitions found" if files.empty?

modules = {}
files.each do |file|
  definition = YAML.safe_load(file.read, permitted_classes: [], aliases: false)
  abort "#{file} must contain a mapping" unless definition.is_a?(Hash)
  name = definition["name"]
  abort "#{file} has an invalid Module name" unless name.is_a?(String) && name.match?(/\A[a-z0-9](?:[-a-z0-9]*[a-z0-9])?\z/)
  abort "#{file} name must match its filename" unless file.basename(".yaml").to_s == name
  abort "duplicate Module #{name}" if modules.key?(name)
  modules[name] = [file, definition]
end

def strings(value, output = [])
  case value
  when Hash
    value.each_value { |child| strings(child, output) }
  when Array
    value.each { |child| strings(child, output) }
  when String
    output << value
  end
  output
end

modules.each do |name, (file, definition)|
  variables = definition.fetch("variables", {})
  abort "#{file} variables must be a mapping" unless variables.is_a?(Hash)
  variables.each do |variable, config|
    abort "#{file} variable #{variable} must be a mapping" unless config.is_a?(Hash)
    abort "#{file} variable #{variable} has unsupported type" unless %w[string options stringPreset].include?(config["type"])
  end

  strings(definition).each do |value|
    value.scan(/\{\{\s*moduleVariable\.([A-Za-z0-9_-]+)\s*\}\}/).flatten.each do |reference|
      abort "#{file} references undefined variable #{reference}" unless variables.key?(reference)
    end
  end

  entities = definition.fetch("entities", [])
  abort "#{file} entities must be a list" unless entities.is_a?(Array)
  entities.each_with_index do |entity, index|
    abort "#{file} entity #{index} must be helmChart" unless entity.is_a?(Hash) && entity["type"] == "helmChart"
    chart = entity["helmChart"]
    abort "#{file} entity #{index} helmChart must be a mapping" unless chart.is_a?(Hash)
    product = chart["productId"]
    abort "#{file} entity #{index} has invalid productId" unless product.is_a?(String) && product.match?(/\A[^:\s]+:[^:\s]+\z/)
    abort "#{file} entity #{index} must use MERGE config ownership" unless chart["syncConfigOverrides"] == "MERGE"
    if chart["configOverrides"]
      rendered = chart["configOverrides"].gsub(/\{\{[^}]+\}\}/, "fixture")
      parsed = YAML.safe_load(rendered, permitted_classes: [], aliases: false)
      abort "#{file} entity #{index} configOverrides must be a version mapping" unless parsed.is_a?(Hash)
    end
  end

  submodules = definition.fetch("submodules", [])
  abort "#{file} submodules must be a list" unless submodules.is_a?(Array)
  submodules.each_with_index do |submodule, index|
    child_name = submodule["name"]
    child = modules[child_name]
    abort "#{file} submodule #{index} is not present in the module directory" unless child
    abort "#{file} submodule #{child_name} must use version: local" unless submodule["version"] == "local"
    child_variables = child[1].fetch("variables", {})
    submodule.fetch("variableOverrides", {}).each_key do |override|
      abort "#{file} overrides undefined #{child_name} variable #{override}" unless child_variables.key?(override)
    end
  end

  abort "#{file} must define entities or submodules" if entities.empty? && submodules.empty?
end

puts "Apollo Module definitions validated: #{modules.length} modules, local composition closed"
