#!/usr/bin/env ruby
# frozen_string_literal: true

require "pathname"
require "yaml"

root = Pathname.new(__dir__).join("../..").expand_path
snapshot = root.join("docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml")
adapter = root.join("internal/controlplane/apollo_api.go")
tests = root.join("internal/controlplane/apollo_api_test.go")

document = YAML.unsafe_load_file(snapshot)
methods = %w[get post put patch delete]
expected = document.fetch("paths").flat_map do |path, item|
  item.filter_map do |method, operation|
    next unless methods.include?(method)
    [method.upcase, path, operation.fetch("operationId")]
  end
end.sort

# Routes now live in a table generated from this same snapshot, so this scan
# reads the generated file rather than the adapter. It is deliberately kept:
# scripts/verify-apollo-routes.sh proves the file was generated, and this proves
# the operation set it carries is still the one pinned here. The adapter is
# checked below for a handler per operation, which is the half generation cannot
# supply.
routes_file = root.join("internal/apollo/routes.gen.go")
registered = routes_file.read.scan(/Pattern: "(GET|POST|PUT|PATCH|DELETE) ([^"]+)"/)
registered = registered.map { |method, path| [method, path] }.sort
adapter_source = adapter.read
handlers = adapter_source[/func \(api \*apolloPublicAPI\) operationHandlers\(\).*?\n}/m].to_s
expected_routes = expected.map { |method, path, _operation| [method, path] }.sort

missing = expected_routes - registered
extra = registered - expected_routes
abort "Apollo public API adapter is missing routes: #{missing.inspect}" unless missing.empty?
abort "Apollo public API adapter has unpinned routes: #{extra.inspect}" unless extra.empty?

expected.each do |_method, _path, operation|
  abort "Apollo public operation #{operation} has no handler binding" unless handlers.include?("\"#{operation}\"")
end

test_source = tests.read
expected.each do |_method, _path, operation|
  # Every operation is covered either directly by its resource test or by the
  # paired list/get/status test named after the public resource. Keep operation
  # IDs in the test source as an auditable coverage declaration.
  abort "Apollo public operation #{operation} has no conformance declaration" unless test_source.include?(operation)
end

# Platform-native immutable configuration is deliberately carried by a /v1
# route. Keep the pinned public publishHelmChart decoder free of that extension;
# otherwise a passing Platform test could silently widen the public claim.
public_helm_handler = adapter_source[/func \(api \*apolloPublicAPI\) publishHelmChart\(.*?^}/m].to_s
abort "Apollo public publishHelmChart handler includes Platform-native platformBundle" if public_helm_handler.include?("PlatformBundle") || public_helm_handler.include?("platformBundle")
boundary_test = "TestApolloPublicAPIPublishHelmChartDoesNotAdmitPlatformNativeBundleFields"
abort "Apollo public/native Helm publication boundary lacks #{boundary_test}" unless test_source.include?(boundary_test)

puts "Apollo public API adapter covers #{expected.length}/#{expected.length} pinned operations"
