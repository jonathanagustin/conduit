#!/usr/bin/env ruby
# frozen_string_literal: true

# The Zarf package deploys hand-written manifests, and the signed Helm charts
# are a second definition of the same runtime (#828). This gate keeps the two
# from drifting: for each Zarf profile it resolves the manifests exactly as a
# deployment would, renders the charts with the values that profile stands
# for, and fails on any difference that is not listed, with a reason, in
# deploy/zarf/chart-equivalence.yaml. A listed difference that no longer
# occurs fails as well, so the list cannot outlive what it excuses.
#
# HELM_BIN names the helm executable (default: helm on PATH). When ZARF_BIN is
# set, the script's own placeholder substitution is also checked against
# `zarf dev inspect manifests`, so the emulation cannot drift from the tool.
#
# PRINT_DIFFERENCES=1 prints every difference instead of judging them.

require "open3"
require "tmpdir"
require "yaml"
require_relative "lib/safe_yaml"

root = File.expand_path("..", __dir__)
helm = ENV.fetch("HELM_BIN", "helm")
zarf_directory = File.join(root, "deploy/zarf")
definition = YAML.safe_load(File.read(File.join(zarf_directory, "zarf.yaml")))
contract = ConduitSafeYAML.file(File.join(zarf_directory, "chart-equivalence.yaml"))
components = definition.fetch("components").to_h { |item| [item["name"], item] }
declared = definition.fetch("variables").to_h { |item| [item["name"], item] }
PLACEHOLDER = /###ZARF_(VAR|CONST)_([A-Z0-9_]+)###/

def run!(*command, env: {})
  stdout, stderr, status = Open3.capture3(env, *command)
  abort "command failed: #{command.join(' ')}\n#{stdout}\n#{stderr}" unless status.success?
  stdout
end

# Zarf runs each action as `sh -e -c <cmd>` with every variable exported as
# ZARF_VAR_<NAME>, and stores trimmed stdout in each setVariables name
# (zarf v0.82.0 src/pkg/packager/actions/actions.go). `unsetenv_others` keeps
# this process's environment out, so a command that passes here does not pass
# only because of a variable the gate happened to inherit.
def run_action(action, variables)
  env = variables.to_h { |name, value| ["ZARF_VAR_#{name}", value] }.merge("PATH" => ENV.fetch("PATH"))
  Open3.capture3(env, "sh", "-e", "-c", action.fetch("cmd"), unsetenv_others: true)
end

def before_actions(component)
  component.dig("actions", "onDeploy", "before") || []
end

def resolve_variables(component, variables)
  before_actions(component).each_with_object(variables.dup) do |action, resolved|
    stdout, stderr, status = run_action(action, resolved)
    abort "#{component['name']} action failed for #{variables['REPOSITORY_KIND']}: #{action['cmd']}\n#{stderr}" unless status.success?
    (action["setVariables"] || []).each { |item| resolved[item.fetch("name")] = stdout.strip }
  end
end

def substitute(text, variables, constants)
  text.gsub(PLACEHOLDER) do
    source = Regexp.last_match(1) == "VAR" ? variables : constants
    source.fetch(Regexp.last_match(2)) { abort "manifest uses an unresolved Zarf placeholder: #{Regexp.last_match(0)}" }
  end
end

# Lists of named mappings (env, volumes, ports, containers) become mappings by
# name, so reordering is not a difference and each entry has a stable path.
def normalize(value)
  case value
  when Hash then value.to_h { |key, item| [key.to_s, normalize(item)] }
  when Array
    if !value.empty? && value.all? { |item| item.is_a?(Hash) && item["name"].is_a?(String) } && value.map { |item| item["name"] }.uniq.length == value.length
      value.to_h { |item| ["[#{item['name']}]", normalize(item.reject { |key, _| key == "name" })] }
    else
      value.map { |item| normalize(item) }
    end
  else value
  end
end

# Reports the highest path at which the two sides part: a key present on one
# side only is one difference, not one per leaf beneath it.
def differences(zarf, chart, path = "")
  if zarf.is_a?(Hash) && chart.is_a?(Hash)
    return (zarf.keys | chart.keys).sort.flat_map do |key|
      child = key.start_with?("[") || path.empty? ? "#{path}#{key}" : "#{path}.#{key}"
      if !chart.key?(key) then [[child, "zarf-only"]]
      elsif !zarf.key?(key) then [[child, "chart-only"]]
      else differences(zarf[key], chart[key], child)
      end
    end
  end
  zarf == chart ? [] : [[path, "differs"]]
end

# Drops the chart's bookkeeping labels wherever labels or selectors appear, so
# one reviewed list covers them instead of one allowlist line per object.
def without_labels(value, keys)
  case value
  when Hash
    value.to_h do |key, item|
      item = item.reject { |label, _| keys.include?(label) } if %w[labels matchLabels selector].include?(key) && item.is_a?(Hash)
      [key, without_labels(item, keys)]
    end
  when Array then value.map { |item| without_labels(item, keys) }
  else value
  end
end

def object_name(item)
  "#{item['kind']}/#{item.dig('metadata', 'name')}"
end

def objects(documents, labels, renames = {})
  documents.compact.to_h do |item|
    name = renames.fetch(object_name(item), object_name(item))
    item = item.merge("metadata" => item["metadata"].merge("name" => name.split("/", 2).last))
    [name, normalize(without_labels(item, labels))]
  end
end

def compare(zarf_documents, chart_documents, labels, renames)
  zarf = objects(zarf_documents, labels)
  chart = objects(chart_documents, labels, renames)
  (zarf.keys | chart.keys).sort.flat_map do |name|
    if !chart.key?(name) then [[name, "", "zarf-only"]]
    elsif !zarf.key?(name) then [[name, "", "chart-only"]]
    else differences(zarf[name], chart[name]).map { |path, side| [name, path, side] }
    end
  end
end

constants = {
  "HUB_IMAGE" => "ghcr.io/example/platform-hub:0.0.0@sha256:#{'a' * 64}",
  "SPOKE_AGENT_IMAGE" => "ghcr.io/example/platform-spoke-agent:0.0.0@sha256:#{'b' * 64}"
}
defaults = declared.transform_values { |item| item["default"].to_s }
derived = before_actions(components.fetch("runtime")).flat_map { |action| (action["setVariables"] || []).map { |item| item["name"] } }
abort "runtime component must derive exactly HUB_REPLICAS, HUB_STRATEGY and HUB_STATE_VOLUME" unless derived.sort == %w[HUB_REPLICAS HUB_STATE_VOLUME HUB_STRATEGY]
abort "derived Hub settings must not also be operator-settable variables" unless (derived & declared.keys).empty?
abort "REPOSITORY_KIND must be the profile switch, constrained to file or postgres" unless declared.dig("REPOSITORY_KIND", "pattern") == "^(file|postgres)$"

runtime_text = File.read(File.join(zarf_directory, "manifests/runtime.yaml"))
worker_text = File.read(File.join(zarf_directory, "manifests/packaging-worker.yaml"))
profiles = contract.fetch("profiles")
abort "chart equivalence must cover exactly the file and postgres profiles" unless profiles.keys.sort == %w[file postgres]
chart_labels = contract.fetch("chartOnlyLabels")
abort "chartOnlyLabels needs keys and a reason" unless chart_labels["keys"].is_a?(Array) && chart_labels["reason"].to_s.strip.length >= 20
abort "chartOnlyLabels must not hide the label the package selects Pods by" if chart_labels["keys"].include?("app.kubernetes.io/name")
renames = contract.fetch("chartObjectNames").to_h do |entry|
  abort "chartObjectNames entry needs chart, zarf and a reason: #{entry.inspect}" unless entry["reason"].to_s.strip.length >= 20
  entry.values_at("chart", "zarf")
end
allowed = contract.fetch("allow")
allowed.each do |entry|
  abort "allowlist entry needs object, path, side, profiles and a reason: #{entry.inspect}" unless
    %w[object path side profiles reason].all? { |key| entry.key?(key) } && entry["reason"].to_s.strip.length >= 20 &&
    %w[zarf-only chart-only differs].include?(entry["side"]) && (entry["profiles"] - profiles.keys).empty? && !entry["profiles"].empty?
end
abort "allowlist entries must be unique" unless allowed.map { |entry| entry.values_at("object", "path", "side") }.uniq.length == allowed.length

failures = []
Dir.mktmpdir("zarf-chart-equivalence") do |work|
  profiles.each do |profile, settings|
    kind = settings.fetch("repositoryKind")
    abort "profile #{profile} must select itself with REPOSITORY_KIND" unless kind == profile
    base = defaults.merge(settings.fetch("zarfVariables")).merge("REPOSITORY_KIND" => kind)
    variables = resolve_variables(components.fetch("runtime"), base)
    worker = settings["packagingWorker"]

    # The worker's deploy-time guard, run for real: it must refuse exactly the
    # profile whose Hub would never read what the worker writes.
    _stdout, stderr, status = run_action(before_actions(components.fetch("packaging-worker")).fetch(0) { abort "packaging-worker must carry a deploy-time profile guard" }, variables)
    if kind == "postgres"
      abort "packaging-worker guard refused the postgres profile: #{stderr}" unless status.success?
    else
      abort "packaging-worker guard accepted a #{kind}-backed Hub" if status.success?
      abort "packaging-worker guard must say why it refused" unless stderr.include?("REPOSITORY_KIND=postgres")
    end
    abort "profile #{profile} selects the packaging worker beside a #{kind}-backed Hub" if worker && kind != "postgres"

    rendered = substitute(runtime_text, variables, constants)
    zarf_documents = ConduitSafeYAML.stream(rendered)
    zarf_documents += ConduitSafeYAML.stream(substitute(worker_text, variables, constants)) if worker

    hub = zarf_documents.find { |item| item["kind"] == "Deployment" && item.dig("metadata", "name") == "platform-delivery" }
    state = hub.dig("spec", "template", "spec", "volumes").find { |item| item["name"] == "state" }
    budget = zarf_documents.find { |item| item["kind"] == "PodDisruptionBudget" }
    abort "Hub PodDisruptionBudget must select the Hub Pods and keep all but one" unless
      budget&.dig("spec") == {"maxUnavailable" => 1, "selector" => {"matchLabels" => hub.dig("spec", "selector", "matchLabels")}}
    if kind == "postgres"
      abort "postgres profile must run two Hub replicas" unless hub.dig("spec", "replicas") == 2
      abort "postgres profile must roll without losing a serving Hub" unless
        hub.dig("spec", "strategy") == {"type" => "RollingUpdate", "rollingUpdate" => {"maxUnavailable" => 0, "maxSurge" => 1}}
      abort "postgres profile must not share a ReadWriteOnce claim between replicas" unless state == {"name" => "state", "emptyDir" => {}}
    else
      abort "file profile must run exactly one Hub replica" unless hub.dig("spec", "replicas") == 1
      abort "file profile must stop the old Hub before starting the new one" unless hub.dig("spec", "strategy") == {"type" => "Recreate"}
      abort "file profile must keep Hub state on the retained claim" unless
        state == {"name" => "state", "persistentVolumeClaim" => {"claimName" => "platform-delivery-state"}}
    end

    if ENV.key?("ZARF_BIN")
      zarf = ENV["ZARF_BIN"]
      abort "ZARF_BIN must name a Zarf executable" if zarf.to_s.empty?
      # stringToString flags are CSV records: quote each so commas and braces
      # in a derived value stay inside one KEY=value.
      deploy_set = variables.reject { |_, value| value.empty? }.flat_map { |name, value| ["--deploy-set-variables", %("#{name}=#{value.gsub('"', '""')}")] }
      create_set = constants.merge("PLATFORM_VERSION" => "0.0.0").flat_map { |name, value| ["--create-set", "#{name}=#{value}"] }
      inspected = run!(zarf, "dev", "inspect", "manifests", zarf_directory, "--architecture", "amd64", "--no-color", *create_set, *deploy_set,
                       env: {"ZARF_NO_PROGRESS" => "true"})
      abort "zarf dev inspect manifests left a placeholder unresolved for #{profile}" if
        inspected.lines.reject { |line| line.lstrip.start_with?("#") }.join.match?(PLACEHOLDER)
      by_tool = ConduitSafeYAML.stream(inspected).compact.select { |item| item.is_a?(Hash) }
      zarf_documents.each do |document|
        match = by_tool.find { |item| item["kind"] == document["kind"] && item.dig("metadata", "name") == document.dig("metadata", "name") }
        abort "Zarf templated #{document['kind']}/#{document.dig('metadata', 'name')} differently from this gate for #{profile}" unless match == document
      end
    end

    hub_values = File.join(work, "#{profile}-hub.yaml")
    spoke_values = File.join(work, "#{profile}-spoke.yaml")
    File.write(hub_values, YAML.dump(settings.fetch("hubValues")))
    File.write(spoke_values, YAML.dump(settings.fetch("spokeValues")))
    chart_documents =
      ConduitSafeYAML.stream(run!(helm, "template", "platform-delivery", File.join(root, "deploy/helm/platform-hub"), "--namespace", "platform-system", "--values", hub_values)) +
      ConduitSafeYAML.stream(run!(helm, "template", "platform-spoke-agent", File.join(root, "deploy/helm/platform-spoke"), "--namespace", "platform-system", "--values", spoke_values))

    stale = chart_labels["keys"].reject { |key| chart_documents.to_s.include?(%("#{key}"=>)) }
    abort "chartOnlyLabels lists labels the charts no longer render: #{stale.join(', ')}" unless stale.empty?
    found = compare(zarf_documents, chart_documents, chart_labels["keys"], renames)
    if worker
      unused = renames.keys - chart_documents.compact.map { |item| object_name(item) }
      abort "chartObjectNames lists objects the charts do not render: #{unused.join(', ')}" unless unused.empty?
    end
    if ENV["PRINT_DIFFERENCES"] == "1"
      found.each { |object, path, side| puts [profile, side, object, path].join("\t") }
      next
    end
    expected = allowed.select { |entry| entry["profiles"].include?(profile) }
    found.each do |object, path, side|
      next if expected.any? { |entry| entry.values_at("object", "path", "side") == [object, path, side] }
      failures << "#{profile}: #{object} #{path.empty? ? '(whole object)' : path} #{side} is not an allowed difference between the Zarf manifest and the chart"
    end
    expected.each do |entry|
      next if found.include?(entry.values_at("object", "path", "side"))
      failures << "#{profile}: allowed difference no longer occurs and must be removed: #{entry['object']} #{entry['path']} #{entry['side']}"
    end
  end
end
exit 0 if ENV["PRINT_DIFFERENCES"] == "1"
abort "Zarf manifests and Helm charts drifted:\n  #{failures.join("\n  ")}" unless failures.empty?

puts "Zarf manifests match the Helm chart render for the file and postgres profiles, apart from #{allowed.length} listed differences"
