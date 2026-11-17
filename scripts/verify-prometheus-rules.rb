#!/usr/bin/env ruby
# frozen_string_literal: true

require "yaml"
require "tmpdir"
require "open3"

root = ENV.fetch("CONDUIT_OBSERVABILITY_ROOT", File.expand_path("..", __dir__))
tool = ENV.fetch("PROMTOOL_BIN", "promtool")
version, status = Open3.capture2e(tool, "--version")
abort "promtool 3.16.0 is required: #{version}" unless status.success? && version.match?(/\bversion 3\.16\.0\s/)

# Prometheus consumes a rule group document, while the shipped source is the
# Kubernetes PrometheusRule wrapper. Extract its groups without rewriting any
# expression, threshold, label, annotation or duration.
source = YAML.safe_load_file(File.join(root, "deploy/observability/prometheus-rules.yaml"))
groups = source.fetch("spec").fetch("groups")
fixtures = YAML.safe_load_file(File.join(root, "deploy/observability/prometheus-rules.test.yml"))
Dir.mktmpdir("conduit-prometheus-rules-") do |dir|
  File.write(File.join(dir, "rules.yaml"), YAML.dump({ "groups" => groups }))
  File.write(File.join(dir, "tests.yaml"), YAML.dump(fixtures))
  Dir.chdir(dir) do
    abort "Prometheus rule validation failed" unless system(tool, "check", "rules", "rules.yaml")
    abort "Prometheus alert behavior failed" unless system(tool, "test", "rules", "tests.yaml")
  end
end
