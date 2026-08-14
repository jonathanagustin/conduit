#!/usr/bin/env ruby

require "date"
require "yaml"

abort "usage: validate-public-api.rb API_DEFINITION.yml" unless ARGV.length == 1
document = YAML.safe_load_file(ARGV.fetch(0), permitted_classes: [Date, Time], aliases: true)

abort "expected OpenAPI 3.0.1" unless document.fetch("openapi") == "3.0.1"
schemas = document.dig("components", "schemas") || abort("components.schemas missing")

def resolve_ref(document, ref)
  abort "external OpenAPI ref is outside this validation: #{ref}" unless ref.start_with?("#/")
  ref.delete_prefix("#/").split("/").reduce(document) do |node, segment|
    node.fetch(segment.gsub("~1", "/").gsub("~0", "~"))
  end
end

refs = []
walk = lambda do |value|
  case value
  when Hash
    refs << value.fetch("$ref") if value.key?("$ref")
    value.each_value { |child| walk.call(child) }
  when Array
    value.each { |child| walk.call(child) }
  end
end
walk.call(document)
refs.uniq.each { |ref| resolve_ref(document, ref) }

helm = schemas.fetch("HelmChartProductExtensions")
abort "helmChart must be required" unless helm.fetch("required").include?("helmChart")
dependencies = schemas.fetch("ProductDependencyLocator")
expected = %w[productGroup productName minimumVersion maximumVersion]
abort "ProductDependencyLocator changed" unless dependencies.fetch("required").sort == expected.sort

schemes = document.dig("components", "securitySchemes") || {}
abort "BearerAuth missing" unless schemes.key?("BearerAuth")
abort "OAuth2 missing" unless schemes.key?("OAuth2")

puts "Apollo public OpenAPI validated: #{document.fetch("paths", {}).length} paths, " \
     "#{schemas.length} schemas, #{refs.uniq.length} local refs"
