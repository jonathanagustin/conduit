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

# The old worker-only default was loopback. Once the Hub reads the setting,
# retaining it would turn on a client in every plain install.
packaging_variables = %w[PACKAGING_TEMPORAL_ADDRESS PACKAGING_TEMPORAL_NAMESPACE PACKAGING_TEMPORAL_TASK_QUEUE]
abort "packaging Temporal must default to disabled with its established namespace and queue" unless
  defaults.values_at(*packaging_variables) == ["", "default", "packaging-localhost"]
packaging_patterns = packaging_variables.to_h do |name|
  pattern = declared.dig(name, "pattern")
  abort "#{name} must constrain plain deployment configuration" if pattern.to_s.empty?
  [name, Regexp.new("\\A(?:#{pattern})\\z")]
end
{
  "PACKAGING_TEMPORAL_ADDRESS" => ["", "temporal.example.test:7233", "127.0.0.1:7233", "temporal:1", "temporal:65535", "temporal:0007233", "münchen.example:7233"],
  "PACKAGING_TEMPORAL_NAMESPACE" => ["default", "packaging-production", "a" * 128, "namespace with spaces"],
  "PACKAGING_TEMPORAL_TASK_QUEUE" => ["packaging-localhost", "packaging-reviewed", "q" * 300, "queue/with spaces"]
}.each do |name, values|
  abort "#{name} refuses a supported plain setting" unless values.all? { |value| packaging_patterns.fetch(name).match?(value) }
end
abort "PACKAGING_TEMPORAL_ADDRESS must preserve bracketed IPv6 endpoints and interface zones" unless
  ["[fd00::1]:7233", "[fe80::1%eth0]:7233", "[::ffff:192.0.2.1]:7233"].all? do |address|
    packaging_patterns.fetch("PACKAGING_TEMPORAL_ADDRESS").match?(address)
  end
{
  "PACKAGING_TEMPORAL_ADDRESS" => ["https://temporal.example.test:7233", "user:secret@temporal:7233", "temporal:7233?key=secret", "temporal:7233\nOTHER=value", "fd00::1:7233"],
  "PACKAGING_TEMPORAL_NAMESPACE" => ["", "namespace\nOTHER=value", 'namespace"', "namespace\\escape", "namespace\u0085rewrite"],
  "PACKAGING_TEMPORAL_TASK_QUEUE" => ["", "queue\nOTHER=value", 'queue"', "queue\\escape", "queue\u0085rewrite"]
}.each do |name, values|
  abort "#{name} accepts a credential, URI or unsafe manifest value" if values.any? { |value| packaging_patterns.fetch(name).match?(value) }
end
abort "PACKAGING_TEMPORAL_ADDRESS must refuse ports outside 1 through 65535" if
  ["temporal:0", "temporal:65536", "temporal:99999", "[fd00::1]:65536"].any? do |address|
    packaging_patterns.fetch("PACKAGING_TEMPORAL_ADDRESS").match?(address)
  end
packaging_profile_guard = before_actions(components.fetch("runtime")).find do |action|
  action["description"] == "Refuse packaging workflow configuration beside a file-backed Hub"
end
abort "runtime must guard packaging configuration beside a file-backed Hub" unless packaging_profile_guard
[["file", "", true], ["postgres", "", true], ["postgres", "temporal:7233", true], ["file", "temporal:7233", false]].each do |kind, address, succeeds|
  _stdout, stderr, status = run_action(packaging_profile_guard, defaults.merge("REPOSITORY_KIND" => kind, "PACKAGING_TEMPORAL_ADDRESS" => address))
  abort "runtime packaging guard must refuse only a configured file-backed Hub" unless status.success? == succeeds
  abort "runtime packaging guard must explain the shared Plan-store requirement" if !succeeds && !stderr.include?("requires REPOSITORY_KIND=postgres")
end
packaging_worker_guard = before_actions(components.fetch("packaging-worker")).find do |action|
  action["description"] == "Refuse the packaging worker beside a file-backed Hub"
end
abort "packaging-worker must carry a deploy-time profile guard" unless packaging_worker_guard
packaging_address_guard = before_actions(components.fetch("packaging-worker")).find do |action|
  action["description"] == "Refuse the packaging worker without an explicit Temporal frontend"
end
abort "packaging-worker must require an explicit Temporal frontend" unless packaging_address_guard
["", "temporal:7233"].each do |address|
  _stdout, stderr, status = run_action(packaging_address_guard, defaults.merge("REPOSITORY_KIND" => "postgres", "PACKAGING_TEMPORAL_ADDRESS" => address))
  abort "packaging-worker address guard must refuse an empty frontend" unless status.success? == !address.empty?
  abort "packaging-worker address guard must explain the missing frontend" if address.empty? && !stderr.include?("explicit PACKAGING_TEMPORAL_ADDRESS")
end

runtime_text = File.read(File.join(zarf_directory, "manifests/runtime.yaml"))
worker_text = File.read(File.join(zarf_directory, "manifests/packaging-worker.yaml"))
profiles = contract.fetch("profiles")
abort "chart equivalence must cover exactly the file, postgres and postgres-single profiles" unless profiles.keys.sort == %w[file postgres postgres-single]
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
    abort "profile #{profile} must select itself with REPOSITORY_KIND" unless profile.split("-").first == kind
    shared = !settings.fetch("zarfVariables")["PLANNING_TEMPORAL_ADDRESS"].to_s.empty?
    abort "profile #{profile} must #{profile == 'postgres' ? 'set' : 'leave empty'} PLANNING_TEMPORAL_ADDRESS" unless shared == (profile == "postgres")
    base = defaults.merge(settings.fetch("zarfVariables")).merge("REPOSITORY_KIND" => kind)
    variables = resolve_variables(components.fetch("runtime"), base)
    worker = settings["packagingWorker"]

    # The worker's deploy-time guard, run for real: it must refuse exactly the
    # profile whose Hub would never read what the worker writes.
    _stdout, stderr, status = run_action(packaging_worker_guard, variables)
    if kind == "postgres"
      abort "packaging-worker guard refused the postgres profile: #{stderr}" unless status.success?
    else
      abort "packaging-worker guard accepted a #{kind}-backed Hub" if status.success?
      abort "packaging-worker guard must say why it refused" unless stderr.include?("REPOSITORY_KIND=postgres")
    end
    abort "profile #{profile} selects the packaging worker beside a #{kind}-backed Hub" if worker && kind != "postgres"
    packaging_enabled = !variables.fetch("PACKAGING_TEMPORAL_ADDRESS").empty?
    abort "#{profile}: packaged worker has no explicit Temporal frontend" if worker && !packaging_enabled
    abort "#{profile}: chart and package disagree about packaging client enablement" unless
      !!settings.dig("hubValues", "packagingWorkflow", "enabled") == packaging_enabled

    rendered = substitute(runtime_text, variables, constants)
    zarf_documents = ConduitSafeYAML.stream(rendered)
    zarf_documents += ConduitSafeYAML.stream(substitute(worker_text, variables, constants)) if worker

    hub = zarf_documents.find { |item| item["kind"] == "Deployment" && item.dig("metadata", "name") == "platform-delivery" }
    state = hub.dig("spec", "template", "spec", "volumes").find { |item| item["name"] == "state" }
    budget = zarf_documents.find { |item| item["kind"] == "PodDisruptionBudget" }
    abort "Hub PodDisruptionBudget must select the Hub Pods and keep all but one" unless
      budget&.dig("spec") == {"maxUnavailable" => 1, "selector" => {"matchLabels" => hub.dig("spec", "selector", "matchLabels")}}
    # What the Hub itself enforces at startup (validatePlanningReplicas): it
    # must be told the true count, and more than one needs a shared frontend.
    hub_env = hub.dig("spec", "template", "spec", "containers", 0, "env").to_h { |item| [item["name"], item["value"]] }
    packaging_variables.each do |name|
      abort "#{profile}: Hub must forward the shared #{name}" unless hub_env[name] == variables.fetch(name)
    end
    if worker
      worker_deployment = zarf_documents.find { |item| item["kind"] == "Deployment" && item.dig("metadata", "name") == "platform-packaging-worker" }
      worker_env = worker_deployment.dig("spec", "template", "spec", "containers", 0, "env").to_h { |item| [item["name"], item["value"]] }
      packaging_variables.each do |name|
        abort "#{profile}: Hub and worker must share #{name}" unless worker_env[name] == hub_env.fetch(name)
      end
    end
    abort "#{profile}: Hub is told HUB_REPLICA_COUNT=#{hub_env['HUB_REPLICA_COUNT']} but runs #{hub.dig('spec', 'replicas')} replicas" unless
      hub_env["HUB_REPLICA_COUNT"] == hub.dig("spec", "replicas").to_s
    abort "#{profile}: more than one Hub replica without a shared planning frontend" if
      hub.dig("spec", "replicas") > 1 && (hub_env["CONDUIT_TEMPORAL_ADDRESS"].to_s.empty? || hub_env["CONDUIT_TEMPORAL_SHARED"] != "true")
    if kind == "postgres"
      abort "postgres profile must run two Hub replicas when planning is shared" if shared && hub.dig("spec", "replicas") != 2
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
      if profile == "postgres"
        # Go's POSIX [:cntrl:] class is ASCII; Ruby's also matches C1
        # controls. Probe the actual pinned CLI so Ruby cannot accidentally
        # vouch for a pattern that lets YAML rewrite U+0085 at deployment.
        {
          "PACKAGING_TEMPORAL_ADDRESS" => ["temporal:65536", "user:credential@temporal:7233"],
          "PACKAGING_TEMPORAL_NAMESPACE" => ["namespace\u0085rewrite"],
          "PACKAGING_TEMPORAL_TASK_QUEUE" => ["queue\u0085rewrite"]
        }.each do |name, values|
          values.each do |value|
            rejected = variables.merge(name => value).reject { |_, setting| setting.empty? }.flat_map do |key, setting|
              ["--deploy-set-variables", %("#{key}=#{setting.gsub('"', '""')}")]
            end
            _stdout, stderr, status = Open3.capture3({"ZARF_NO_PROGRESS" => "true"}, zarf, "dev", "inspect", "manifests", zarf_directory,
              "--architecture", "amd64", "--no-color", *create_set, *rejected)
            abort "pinned Zarf accepted unsafe #{name} configuration" if status.success?
            abort "pinned Zarf refused #{name} for an unexpected reason" unless
              stderr.include?(%(provided value for variable "#{name}" does not match pattern))
          end
        end
        # Zl/Zp are not Cc controls. Keep values this exact templating/parser
        # path preserves; the Temporal server remains the naming authority.
        {"PACKAGING_TEMPORAL_NAMESPACE" => "namespace\u2028preserved", "PACKAGING_TEMPORAL_TASK_QUEUE" => "queue\u2029preserved"}.each do |name, value|
          probe = variables.merge(name => value).reject { |_, setting| setting.empty? }.flat_map do |key, setting|
            ["--deploy-set-variables", %("#{key}=#{setting.gsub('"', '""')}")]
          end
          preserved = run!(zarf, "dev", "inspect", "manifests", zarf_directory, "--architecture", "amd64", "--no-color", *create_set, *probe,
            env: {"ZARF_NO_PROGRESS" => "true"})
          documents = ConduitSafeYAML.stream(preserved).compact.select { |item| item.is_a?(Hash) }
          %w[platform-delivery platform-packaging-worker].each do |deployment|
            pod = documents.find { |item| item["kind"] == "Deployment" && item.dig("metadata", "name") == deployment }
            setting = pod.dig("spec", "template", "spec", "containers", 0, "env").find { |entry| entry["name"] == name }
            abort "pinned Zarf/parser rewrote #{name}" unless setting&.fetch("value") == value
          end
        end
      end
    end

    hub_values = File.join(work, "#{profile}-hub.yaml")
    spoke_values = File.join(work, "#{profile}-spoke.yaml")
    File.write(hub_values, YAML.dump(settings.fetch("hubValues")))
    File.write(spoke_values, YAML.dump(settings.fetch("spokeValues")))
    chart_documents =
      ConduitSafeYAML.stream(run!(helm, "template", "platform-delivery", File.join(root, "deploy/helm/platform-hub"), "--namespace", "platform-system", "--values", hub_values)) +
      ConduitSafeYAML.stream(run!(helm, "template", "platform-spoke-agent", File.join(root, "deploy/helm/platform-spoke"), "--namespace", "platform-system", "--values", spoke_values))

    # Compare parsed labels; Hash#inspect spacing differs between Ruby versions.
    stale = chart_labels["keys"].reject { |key| without_labels(chart_documents, [key]) != chart_documents }
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

puts "Zarf manifests match the Helm chart render for the file, postgres and postgres-single profiles, apart from #{allowed.length} listed differences"
