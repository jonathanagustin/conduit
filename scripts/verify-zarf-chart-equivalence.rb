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
require "fileutils"
require "rbconfig"
require "tmpdir"
require "yaml"
require_relative "lib/safe_yaml"
require_relative "lib/zarf_runtime_values"

root = File.expand_path("..", __dir__)
helm = ENV.fetch("HELM_BIN", "helm")
zarf_directory = File.join(root, "deploy/zarf")
definition = ConduitZarfRuntimeValues.definition(zarf_directory, {"HUB_IMAGE" => "fixture-hub", "SPOKE_AGENT_IMAGE" => "fixture-spoke"})
contract = ConduitSafeYAML.file(File.join(zarf_directory, "chart-equivalence.yaml"))
components = definition.fetch("components").to_h { |item| [item["name"], item] }
declared = JSON.parse(File.read(File.join(zarf_directory, "values.schema.json"))).fetch("properties").reject { |name, _| name == "rendered" }
PLACEHOLDER = /###ZARF_(VAR|CONST)_([A-Z0-9_]+)###/

def run!(*command, env: {})
  stdout, stderr, status = Open3.capture3(env, *command)
  abort "command failed: #{command.join(' ')}\n#{stdout}\n#{stderr}" unless status.success?
  abort "command emitted a deprecation warning: #{command.join(' ')}\n#{stderr}" if stderr.match?(/deprecat/i)
  stdout
end

# Execute the shipped command after resolving only the supported template
# expressions, without inheriting operator variables from this process.
def run_action(action, variables)
  command = ConduitZarfRuntimeValues.render(action.fetch("cmd"), variables, action: true)
  Open3.capture3({"PATH" => ENV.fetch("PATH")}, "sh", "-e", "-c", command, unsetenv_others: true)
end

# Operational cases reuse a reviewed baseline, then change only typed settings.
def deep_merge(left, right)
  left.merge(right) { |_, old, value| old.is_a?(Hash) && value.is_a?(Hash) ? deep_merge(old, value) : value }
end

def before_actions(component)
  component.dig("actions", "onDeploy", "before") || []
end

def resolve_variables(component, variables)
  before_actions(component).each_with_object(variables.dup) do |action, resolved|
    stdout, stderr, status = run_action(action, resolved)
    abort "#{component['name']} action failed for #{variables['REPOSITORY_KIND']}: #{action['cmd']}\n#{stderr}" unless status.success?
    (action["setValues"] || []).each { |item| resolved[item.fetch("key").delete_prefix(".rendered.")] = stdout.strip }
  end
end

def substitute(text, variables, constants)
  ConduitZarfRuntimeValues.render(text, variables, constants)
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
defaults = YAML.safe_load_file(File.join(zarf_directory, "values.yaml")).transform_values(&:to_s)
derived = before_actions(components.fetch("runtime")).flat_map { |action| (action["setValues"] || []).map { |item| item.fetch("key").delete_prefix(".rendered.") } }
abort "runtime component must derive exactly HUB_REPLICAS, HUB_STRATEGY and HUB_STATE_VOLUME" unless derived.sort == %w[HUB_OPERATIONAL_ENV HUB_OPERATIONAL_MOUNTS HUB_OPERATIONAL_VOLUMES HUB_REPLICAS HUB_STATE_VOLUME HUB_STRATEGY SPOKE_OPERATIONAL_ENV SPOKE_OPERATIONAL_MOUNTS SPOKE_OPERATIONAL_VOLUMES]
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
profiles = profiles.dup
contract.fetch("operationalProfiles").each do |name, settings|
  base = settings.fetch("baseProfile")
  abort "operational profile #{name} needs a reviewed baseline" unless profiles.key?(base) && !profiles[base].key?("baseProfile")
  abort "duplicate operational profile #{name}" if profiles.key?(name)
  profiles[name] = deep_merge(profiles.fetch(base), settings)
end
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
    %w[zarf-only chart-only differs].include?(entry["side"]) && (entry["profiles"] - contract.fetch("profiles").keys).empty? && !entry["profiles"].empty?
end
abort "allowlist entries must be unique" unless allowed.map { |entry| entry.values_at("object", "path", "side") }.uniq.length == allowed.length

failures = []
Dir.mktmpdir("zarf-chart-equivalence") do |work|
  profiles.each do |profile, settings|
    kind = settings.fetch("repositoryKind")
    baseline = settings.fetch("baseProfile", profile)
    abort "profile #{profile} must select itself with REPOSITORY_KIND" unless profile.split("-").first == kind
    shared = !settings.fetch("zarfVariables")["PLANNING_TEMPORAL_ADDRESS"].to_s.empty?
    abort "profile #{profile} must #{baseline == 'postgres' ? 'set' : 'leave empty'} PLANNING_TEMPORAL_ADDRESS" unless shared == (baseline == "postgres")
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
      compiled = File.join(work, "zarf.gen.yaml")
      create_set = constants.merge("PLATFORM_VERSION" => "0.0.0").flat_map { |name, value| ["--set", "#{name}=#{value}"] }
      template = File.join(work, "zarf.tpl.yaml")
      FileUtils.cp(File.join(zarf_directory, "zarf.tpl.yaml"), template)
      run!(zarf, "dev", "template", template, *create_set)
      # Manifest paths remain relative to the compiled package definition.
      FileUtils.cp_r(File.join(zarf_directory, "manifests"), work) unless File.directory?(File.join(work, "manifests"))
      FileUtils.cp(File.join(zarf_directory, "values.yaml"), work)
      FileUtils.cp(File.join(zarf_directory, "values.schema.json"), work)
      values_file = File.join(work, "profile-values.yaml")
      File.write(values_file, JSON.generate(ConduitZarfRuntimeValues.nested(variables, derived)))
      inspected = run!(zarf, "dev", "inspect", "manifests", compiled, "--architecture", "amd64", "--no-color", "--values", values_file,
                       env: {"ZARF_NO_PROGRESS" => "true"})
      abort "native Zarf emitted a deprecation warning" if inspected.match?(/deprecat/i)
      abort "zarf dev inspect manifests left a placeholder unresolved for #{profile}" if
        inspected.lines.reject { |line| line.lstrip.start_with?("#") }.join.match?(PLACEHOLDER)
      by_tool = ConduitSafeYAML.stream(inspected).compact.select { |item| item.is_a?(Hash) }
      zarf_documents.each do |document|
        match = by_tool.find { |item| item["kind"] == document["kind"] && item.dig("metadata", "name") == document.dig("metadata", "name") }
        abort "Zarf templated #{document['kind']}/#{document.dig('metadata', 'name')} differently from this gate for #{profile}" unless match == document
      end
      if profile == "postgres"
        [false, true].each do |metrics|
          selected = variables.merge("UDS_METRICS" => metrics, "METRICS_ADDRESS" => ":9090")
          File.write(values_file, JSON.generate(ConduitZarfRuntimeValues.nested(selected, derived)))
          native = run!(zarf, "dev", "inspect", "manifests", compiled, "--architecture", "amd64", "--no-color", "--values", values_file)
          owners = ConduitSafeYAML.stream(native).compact.select { |item| item.is_a?(Hash) && item["kind"] == "Package" }
          abort "UDS metrics selection must render exactly one Package owner" unless owners.length == 1
          abort "UDS monitor does not follow the typed metrics setting" unless owners.first.fetch("spec").key?("monitor") == metrics
          expected_owner = YAML.safe_load(substitute(File.read(File.join(zarf_directory, "manifests/uds-package.yaml")), selected, constants))
          abort "UDS native template differs from the parity helper" unless owners.first == expected_owner
        end
        # Go's POSIX [:cntrl:] class is ASCII; Ruby's also matches C1
        # controls. Probe the actual pinned CLI so Ruby cannot accidentally
        # vouch for a pattern that lets YAML rewrite U+0085 at deployment.
        {
          "PACKAGING_TEMPORAL_ADDRESS" => ["temporal:65536", "user:credential@temporal:7233"],
          "PACKAGING_TEMPORAL_NAMESPACE" => ["namespace\u0085rewrite"],
          "PACKAGING_TEMPORAL_TASK_QUEUE" => ["queue\u0085rewrite"]
        }.each do |name, values|
          values.each do |value|
            File.write(values_file, JSON.generate(ConduitZarfRuntimeValues.nested(variables.merge(name => value), derived)))
            _stdout, stderr, status = Open3.capture3({"ZARF_NO_PROGRESS" => "true"}, zarf, "dev", "inspect", "manifests", compiled,
              "--architecture", "amd64", "--no-color", "--values", values_file)
            abort "pinned Zarf accepted unsafe #{name} configuration" if status.success?
            abort "pinned Zarf refused #{name} for an unexpected reason" unless
              stderr.include?(name) && stderr.match?(/pattern|schema/)
          end
        end
        # Zl/Zp are not Cc controls. Keep values this exact templating/parser
        # path preserves; the Temporal server remains the naming authority.
        {"PACKAGING_TEMPORAL_NAMESPACE" => "namespace\u2028preserved", "PACKAGING_TEMPORAL_TASK_QUEUE" => "queue\u2029preserved"}.each do |name, value|
          File.write(values_file, JSON.generate(ConduitZarfRuntimeValues.nested(variables.merge(name => value), derived)))
          preserved = run!(zarf, "dev", "inspect", "manifests", compiled, "--architecture", "amd64", "--no-color", "--values", values_file,
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
    expected = allowed.select { |entry| entry["profiles"].include?(baseline) }
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

if ENV.key?("ZARF_BIN")
  print run!(RbConfig.ruby, File.join(root, "scripts/test-zarf-operational-settings.rb"))
end

puts "Zarf manifests match the Helm chart render for #{profiles.length} baseline and operational profiles, apart from #{allowed.length} listed baseline differences"
