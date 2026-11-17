#!/usr/bin/env ruby
# frozen_string_literal: true

# Holds the alert rules, the dashboard and the runbooks to what the Hub
# actually exports.
#
# An alert on a series that does not exist never fires, and looks exactly like
# an alert on a healthy system. A dashboard panel on one shows "No data", which
# reads as "nothing is happening". Neither is caught by YAML or JSON parsing,
# and neither is caught in review once the metric has been renamed in Go. So:
#
#   - deploy/observability/exported-metrics.txt lists every metric family, and
#     a Go test (TestTheExportedMetricsManifestIsWhatTheCodeRenders) fails when
#     it differs from what the code renders;
#   - this script fails when an expression in the rules or the dashboard names
#     a series that is not in that manifest, uses a label the metrics allowlist
#     does not carry, or calls something that is not a PromQL function.
#
# What this does NOT do: evaluate an expression. No pinned promtool is
# available to the gate, so the expressions are tokenised here, not parsed by
# Prometheus. A type error (a vector compared with a matrix), a wrong `for`
# duration or a threshold that is never crossed all pass. The check is on
# names, functions and structure.
#
# It also holds each alert to a runbook section with the five parts an operator
# needs, and holds the two generated copies (the chart's files/ and the Zarf
# manifest) to the source.
#
#   ruby scripts/verify-observability-assets.rb           # verify
#   ruby scripts/verify-observability-assets.rb --write   # regenerate the copies, then verify
#
# CONDUIT_OBSERVABILITY_ROOT points it at another tree; the self-test uses that.

require "json"
require "yaml"

root = ENV.fetch("CONDUIT_OBSERVABILITY_ROOT", File.expand_path("..", __dir__))
write = ARGV.include?("--write")

MANIFEST = "deploy/observability/exported-metrics.txt"
RULES = "deploy/observability/prometheus-rules.yaml"
DASHBOARD = "deploy/observability/dashboard.json"
RUNBOOKS = "docs/operations/runbooks.md"
LABEL_ALLOWLIST = "internal/observability/domain/metrics.go"
CHART_RULES = "deploy/helm/platform-hub/files/prometheus-rules.yaml"
CHART_DASHBOARD = "deploy/helm/platform-hub/files/dashboard.json"
ZARF_MANIFEST = "deploy/zarf/manifests/observability.yaml"
RUNBOOK_URL = "https://github.com/jonathanagustin/conduit/blob/main/docs/operations/runbooks.md#"

# The conditions issue #839 names. An alert may be added freely; one of these
# may not quietly disappear.
REQUIRED_ALERTS = %w[
  ConduitHubWedged ConduitReconciliationLag ConduitProposalExhausted
  ConduitNotificationDeliveryExhausted ConduitHTTPServerErrors
  ConduitAgentRateLimited ConduitMetricsAbsent
].freeze
RUNBOOK_PARTS = ["Meaning", "First checks", "Likely causes", "Remediation", "What not to do"].freeze

PROMQL_FUNCTIONS = %w[
  abs absent absent_over_time avg avg_over_time bottomk ceil changes clamp clamp_max clamp_min count
  count_over_time count_values delta deriv exp floor group histogram_quantile idelta increase irate
  label_join label_replace last_over_time ln log10 log2 max max_over_time min min_over_time
  predict_linear quantile quantile_over_time rate resets round scalar sgn sort sort_desc sqrt stddev
  stddev_over_time stdvar stdvar_over_time sum sum_over_time time timestamp topk vector
].freeze
PROMQL_KEYWORDS = %w[and or unless by without on ignoring group_left group_right bool offset Inf NaN inf nan].freeze
# Labels Prometheus attaches to a scraped target. They describe the scrape, not
# the tenant, and the Hub does not set them.
TARGET_LABELS = %w[instance job namespace pod service container endpoint le].freeze

failures = []
read = lambda do |path|
  full = File.join(root, path)
  abort "FAIL #{path} is missing" unless File.file?(full)
  File.read(full)
end

# --- what the code exports ------------------------------------------------
series = {}
read.call(MANIFEST).each_line do |line|
  line = line.strip
  next if line.empty? || line.start_with?("#")
  name, type = line.split
  case type
  when "counter", "gauge" then series[name] = type
  when "histogram" then %w[_bucket _sum _count].each { |suffix| series[name + suffix] = "histogram" }
  else failures << "#{MANIFEST}: #{name} has type #{type.inspect}; expected counter, gauge or histogram"
  end
end
abort "FAIL #{MANIFEST} lists fewer than ten series; this check has stopped reading it" if series.length < 10

allowed_labels = read.call(LABEL_ALLOWLIST)[/var labelNames = map\[string\]struct\{\}\{(.*?)\n\}/m, 1]
  &.scan(/^\s*"([a-z_]+)":\s*\{\},/)&.flatten
abort "FAIL cannot read the label allowlist from #{LABEL_ALLOWLIST}; this check has stopped working" if allowed_labels.nil? || allowed_labels.length < 4
known_labels = allowed_labels + TARGET_LABELS

# --- expressions ----------------------------------------------------------
# Tokenise one PromQL expression into the series, labels and functions it
# names. Not a parser: see the header.
analyse = lambda do |expression|
  text = expression.gsub(/"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'/, '""')
  labels = []
  text = text.gsub(/\{([^}]*)\}/) do
    Regexp.last_match(1).split(",").each do |matcher|
      name = matcher[/\A\s*([A-Za-z_][A-Za-z0-9_]*)/, 1]
      labels << name if name
    end
    " "
  end
  text = text.gsub(/\b(by|without|on|ignoring|group_left|group_right)\s*\(([^)]*)\)/) do
    Regexp.last_match(2).split(",").map(&:strip).reject(&:empty?).each { |name| labels << name }
    " "
  end
  text = text.gsub(/\[[^\]]*\]/, " ")
  metrics = []
  unknown = []
  text.to_enum(:scan, /[A-Za-z_:][A-Za-z0-9_:]*/).each do
    match = Regexp.last_match
    token = match[0]
    # The exponent or unit of a number: 1e6, 5m.
    next if match.begin(0).positive? && text[match.begin(0) - 1] =~ /[0-9.]/
    if match.post_match =~ /\A\s*\(/
      unknown << "#{token}()" unless PROMQL_FUNCTIONS.include?(token)
    elsif !PROMQL_KEYWORDS.include?(token)
      metrics << token
    end
  end
  [metrics.uniq, labels.uniq, unknown.uniq]
end

check_expression = lambda do |where, expression|
  if !expression.is_a?(String) || expression.strip.empty?
    failures << "#{where}: no expression"
    next
  end
  metrics, labels, unknown = analyse.call(expression)
  failures << "#{where}: names no series" if metrics.empty?
  metrics.each do |metric|
    next if series.key?(metric)
    hint = series.key?("#{metric}_bucket") ? " (a histogram; use #{metric}_bucket, _sum or _count)" : ""
    failures << "#{where}: #{metric} is not a series the Hub exports#{hint}; see #{MANIFEST}"
  end
  labels.each do |label|
    next if known_labels.include?(label)
    failures << "#{where}: label #{label} is not on the metrics allowlist in #{LABEL_ALLOWLIST}, so no exported series carries it"
  end
  unknown.each { |function| failures << "#{where}: #{function} is not a PromQL function this check knows" }
end

# --- rules ----------------------------------------------------------------
rules_document = YAML.safe_load(read.call(RULES))
unless rules_document.is_a?(Hash) && rules_document["apiVersion"] == "monitoring.coreos.com/v1" && rules_document["kind"] == "PrometheusRule"
  abort "FAIL #{RULES} is not a monitoring.coreos.com/v1 PrometheusRule"
end
groups = rules_document.dig("spec", "groups")
abort "FAIL #{RULES} has no spec.groups" unless groups.is_a?(Array) && !groups.empty?
runbooks = read.call(RUNBOOKS)
sections = {}
current = nil
runbooks.each_line do |line|
  if (heading = line[/\A## (.+?)\s*\z/, 1])
    current = heading
    sections[current] = +""
  elsif current
    sections[current] << line
  end
end

alerts = []
groups.each do |group|
  failures << "#{RULES}: a group has no name" unless group["name"].is_a?(String) && !group["name"].empty?
  Array(group["rules"]).each do |rule|
    name = rule["alert"]
    unless name.is_a?(String) && name.match?(/\AConduit[A-Za-z0-9]+\z/)
      failures << "#{RULES}: a rule in #{group["name"]} is not an alert named Conduit<Something>"
      next
    end
    where = "#{RULES} #{name}"
    failures << "#{where}: defined twice" if alerts.include?(name)
    alerts << name
    check_expression.call(where, rule["expr"])
    failures << "#{where}: severity must be warning or critical" unless %w[warning critical].include?(rule.dig("labels", "severity"))
    %w[summary description].each do |annotation|
      failures << "#{where}: no #{annotation}" if rule.dig("annotations", annotation).to_s.strip.empty?
    end
    # GitHub's anchor for a heading that is exactly the alert name.
    expected = RUNBOOK_URL + name.downcase
    failures << "#{where}: runbook_url must be #{expected}" unless rule.dig("annotations", "runbook_url") == expected
    section = sections[name]
    if section.nil?
      failures << "#{where}: #{RUNBOOKS} has no '## #{name}' section"
    else
      RUNBOOK_PARTS.each do |part|
        body = section[/^### #{Regexp.escape(part)}\s*\n(.*?)(?=^### |\z)/m, 1]
        failures << "#{RUNBOOKS} #{name}: no '### #{part}' part, or it is empty" if body.nil? || body.strip.empty?
      end
    end
  end
end
(REQUIRED_ALERTS - alerts).each { |name| failures << "#{RULES}: required alert #{name} is missing" }
(sections.keys.grep(/\AConduit[A-Za-z0-9]+\z/) - alerts).each do |name|
  failures << "#{RUNBOOKS}: section #{name} describes an alert that #{RULES} does not define"
end

# --- dashboard ------------------------------------------------------------
dashboard_text = read.call(DASHBOARD)
dashboard = JSON.parse(dashboard_text)
panels = Array(dashboard["panels"]).reject { |panel| panel["type"] == "row" }
abort "FAIL #{DASHBOARD} has fewer than five panels; this check has stopped reading it" if panels.length < 5
failures << "#{DASHBOARD}: needs a uid and a title" if dashboard["uid"].to_s.empty? || dashboard["title"].to_s.empty?
ids = Array(dashboard["panels"]).map { |panel| panel["id"] }
failures << "#{DASHBOARD}: panel ids are not unique" unless ids.uniq.length == ids.length
panels.each do |panel|
  where = "#{DASHBOARD} panel #{panel["title"].inspect}"
  targets = Array(panel["targets"])
  failures << "#{where}: no query" if targets.empty?
  targets.each { |target| check_expression.call(where, target["expr"]) }
end

# --- generated copies -----------------------------------------------------
generated_rules = "# GENERATED from #{RULES} by scripts/verify-observability-assets.rb --write.\n" \
  "# Do not edit: the gate fails when this differs from its source.\n" +
  YAML.dump({"groups" => groups}).sub(/\A---\n/, "")
configmap = {
  "apiVersion" => "v1", "kind" => "ConfigMap",
  "metadata" => {
    "name" => "conduit-hub-dashboard", "namespace" => "platform-system",
    "labels" => {"app.kubernetes.io/name" => "platform-delivery", "grafana_dashboard" => "1"}
  },
  "data" => {"conduit-hub.json" => dashboard_text}
}
zarf_rule = rules_document.merge("metadata" => rules_document.fetch("metadata").merge("namespace" => "platform-system"))
generated_zarf = "# GENERATED from #{RULES} and #{DASHBOARD}\n" \
  "# by scripts/verify-observability-assets.rb --write. Do not edit: the gate\n" \
  "# fails when this differs from its sources.\n" \
  "#\n" \
  "# Applied by the optional `uds-observability` Zarf component. It needs the\n" \
  "# Prometheus Operator CRDs and a Grafana dashboard sidecar, which UDS Core\n" \
  "# provides and a plain Zarf cluster does not.\n" +
  YAML.dump(zarf_rule) + YAML.dump(configmap)
{CHART_RULES => generated_rules, CHART_DASHBOARD => dashboard_text, ZARF_MANIFEST => generated_zarf}.each do |path, expected|
  full = File.join(root, path)
  if write
    File.write(full, expected)
  elsif !File.file?(full) || File.read(full) != expected
    failures << "#{path} is not what its source generates; run ruby scripts/verify-observability-assets.rb --write"
  end
end

unless failures.empty?
  failures.each { |failure| warn "FAIL #{failure}" }
  exit 1
end
puts "PASS observability assets: #{alerts.length} alerts and #{panels.length} dashboard panels name only the " \
  "#{series.length} series the Hub exports (names, labels and functions checked; expressions not evaluated), " \
  "every alert has a five-part runbook, and the chart and Zarf copies match their sources"
