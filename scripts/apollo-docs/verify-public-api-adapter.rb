#!/usr/bin/env ruby
# frozen_string_literal: true

require "pathname"
require "yaml"

root = Pathname.new(__dir__).join("../..").expand_path
snapshot = root.join("docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml")
adapter = root.join("internal/controlplane/apollo_openapi_adapter.go")
legacy_adapter = root.join("internal/controlplane/apollo_api.go")
tests = root.join("internal/controlplane/apollo_api_test.go")

document = YAML.unsafe_load_file(snapshot)
methods = %w[get post put patch delete]
expected = document.fetch("paths").flat_map do |path, item|
  item.filter_map do |method, operation|
    next unless methods.include?(method)
    [method.upcase, path, operation.fetch("operationId")]
  end
end.sort

# scripts/verify-apollo-routes.sh proves this file is a byte-stable generation;
# this second check makes the generated router, strict interface, and concrete
# adapter visibly total over the pinned operation set.
generated_file = root.join("internal/apollo/openapi.gen.go")
generated_source = generated_file.read
adapter_source = adapter.read
legacy_adapter_source = legacy_adapter.read

expected.each do |method, path, operation|
  go_method = operation.sub(/\A./) { |character| character.upcase }
  http_method = method.downcase.capitalize
  route = %Q{m.HandleFunc(http.Method#{http_method}+" "+options.BaseURL+"#{path}"}
  abort "Apollo public operation #{operation} has no generated route" unless generated_source.include?(route)
  signature = /\n\s*#{Regexp.escape(go_method)}\(ctx context\.Context, request /
  abort "Apollo public operation #{operation} has no generated strict method" unless generated_source.match?(signature)
  implementation = /func \(adapter \*apolloOpenAPIAdapter\) #{Regexp.escape(go_method)}\(/
  abort "Apollo public operation #{operation} has no adapter implementation" unless adapter_source.match?(implementation)
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
public_helm_handler = legacy_adapter_source[/func \(api \*apolloPublicAPI\) publishHelmChart\(.*?^}/m].to_s
abort "Apollo public publishHelmChart handler includes Platform-native platformBundle" if public_helm_handler.include?("PlatformBundle") || public_helm_handler.include?("platformBundle")
boundary_test = "TestApolloPublicAPIPublishHelmChartDoesNotAdmitPlatformNativeBundleFields"
abort "Apollo public/native Helm publication boundary lacks #{boundary_test}" unless test_source.include?(boundary_test)

puts "Apollo public API adapter covers #{expected.length}/#{expected.length} pinned operations"
