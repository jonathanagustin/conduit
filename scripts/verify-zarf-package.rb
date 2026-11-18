#!/usr/bin/env ruby
# frozen_string_literal: true

require "yaml"
require "json"
require "open3"
require "tmpdir"
require_relative "lib/safe_yaml"
require_relative "lib/zarf_runtime_values"

root = File.expand_path("..", __dir__)
zarf_directory = File.join(root, "deploy/zarf")
manifest_path = File.join(zarf_directory, "manifests/runtime.yaml")
uds_path = File.join(zarf_directory, "manifests/uds-package.yaml")
bundle_path = File.join(zarf_directory, "bundle.uds.hcl")
version = File.read(File.join(root, "VERSION")).strip
bindings = {"HUB_IMAGE" => "###ZARF_PKG_TMPL_HUB_IMAGE###", "SPOKE_AGENT_IMAGE" => "###ZARF_PKG_TMPL_SPOKE_AGENT_IMAGE###"}
definition = ConduitZarfRuntimeValues.definition(zarf_directory, bindings, "###ZARF_PKG_TMPL_PLATFORM_VERSION###")
schema = JSON.parse(File.read(File.join(zarf_directory, "values.schema.json")))
defaults = YAML.safe_load_file(File.join(zarf_directory, "values.yaml"))
properties = schema.fetch("properties")
abort "native deploy values must reject unknown keys" unless schema["additionalProperties"] == false
abort "Environment and Agent IDs must remain required" unless schema.fetch("required").sort == %w[AGENT_ID ENVIRONMENT_ID]
variables = properties.reject { |name, _| name == "rendered" }.map do |name, property|
  string = (property["anyOf"] || []).find { |item| item["type"] == "string" } || property
  pattern = string["pattern"]
  pattern ||= "^(#{string.fetch('enum').map { |value| Regexp.escape(value) }.join('|')})$" if string["enum"]
  item = {"name" => name, "pattern" => pattern}
  item["default"] = defaults.fetch(name).to_s if defaults.key?(name)
  item
end
# Distinct symbolic bindings preserve the structural security assertions below.
# They are test sentinels, not legacy package variables. Native parity separately
# compares real Zarf output; unsupported template expressions fail this helper.
symbols = variables.to_h { |item| [item.fetch("name"), "###ZARF_VAR_#{item.fetch('name')}###"] }
# A UDS network port is a typed integer, not a quoted symbolic placeholder.
# Namespace/selector bindings remain distinct and the native parity gate tests
# the operator-provided port and sampling types.
symbols["TRACING_COLLECTOR_PORT"] = 4318
properties.fetch("rendered").fetch("properties").each_key { |name| symbols[name] = "###ZARF_VAR_#{name}###" }
image_symbols = bindings.to_h { |name, _| [name, "###ZARF_CONST_#{name}###"] }
render = ->(text, metrics = false) { ConduitZarfRuntimeValues.render(text, symbols.merge("UDS_METRICS" => metrics), image_symbols) }
["zarf.tpl.yaml", "manifests/runtime.yaml", "manifests/packaging-worker.yaml", "manifests/uds-package.yaml"].each do |path|
  abort "native runtime source retains a legacy placeholder: #{path}" if File.read(File.join(zarf_directory, path)).include?("###ZARF_")
end
manifest_text = render.call(File.read(manifest_path))
uds_text = render.call(File.read(uds_path))
metrics_text = render.call(File.read(uds_path), true)
manifests = ConduitSafeYAML.stream(manifest_text)
uds_package = YAML.safe_load(uds_text)
bundle_text = File.read(bundle_path)

abort "Zarf package API must be pinned" unless definition["apiVersion"] == "zarf.dev/v1beta1"
abort "Zarf package version must come from the release" unless definition.dig("metadata", "version") == "###ZARF_PKG_TMPL_PLATFORM_VERSION###"
components = definition.fetch("components")
abort "Zarf package components drifted" unless components.map { |item| [item["name"], item.fetch("optional", false)] } == [["runtime", false], ["packaging-worker", true], ["uds", true], ["uds-observability", true]]
abort "native package must not retain deprecated variable/constant declarations" if definition.key?("variables") || definition.key?("constants")
abort "native components must not retain deprecated selection fields" if components.any? { |item| (item.keys & %w[required default group healthChecks]).any? }
worker = components.find { |item| item["name"] == "packaging-worker" }
abort "optional packaging worker must reuse the Hub image" unless worker["images"] == [{"name" => bindings.fetch("HUB_IMAGE"), "source" => "registry"}]
uds_component = components.find { |item| item["name"] == "uds" }
abort "UDS component must not carry images" if uds_component["images"]
abort "UDS component must apply only the templated Package CR" unless uds_component.dig("manifests", 0, "files") == ["manifests/uds-package.yaml"] && uds_component.dig("manifests", 0, "enableTemplating") == true
runtime_files = components.first.dig("manifests", 0, "files")
abort "runtime component must not apply the UDS Package CR" if runtime_files.any? { |file| file.include?("uds-package") }
images = components.first.fetch("images")
abort "runtime must package exactly the sealed Hub and Agent registry images" unless images == bindings.values.map { |value| {"name" => value, "source" => "registry"} }
guard = (components.first.dig("actions", "onCreate", "before") || []).find { |action| action["description"] == "Require immutable packaged runtime images" }
abort "runtime creation must guard both immutable image bindings" unless guard && !guard["enableTemplating"] &&
  %w[HUB_IMAGE SPOKE_AGENT_IMAGE].all? { |name| guard.fetch("cmd").include?(%([[ printf "%x" .#{name} ]])) && guard.fetch("cmd").include?("#{name} must be digest pinned") } &&
  guard.fetch("cmd").scan("grep -Eq '^.+@sha256:[a-f0-9]{64}$'").length == 2
{"conduit.dev/hub-image" => "HUB_IMAGE", "conduit.dev/spoke-agent-image" => "SPOKE_AGENT_IMAGE"}.each do |annotation, name|
  abort "runtime image annotation differs from its packaged image" unless definition.dig("metadata", "annotations", annotation) == bindings.fetch(name)
end
packaged = image_symbols.to_h { |name, symbol| [symbol, bindings.fetch(name)] }
components.each do |component|
  started = (component["manifests"] || []).flat_map { |item| item.fetch("files") }.flat_map do |file|
    text = File.read(File.join(zarf_directory, file))
    text = render.call(text) unless component["name"] == "uds-observability"
    text.scan(/^\s*image:\s*(.+?)\s*$/).flatten.map { |image| image.delete(%q("')) }
  end.uniq
  started.each do |image|
    abort "#{component['name']} starts an image without its sealed packaged binding: #{image}" unless packaged.key?(image) && (component["images"] || []).any? { |item| item["name"] == packaged.fetch(image) }
  end
end
readme = File.read(File.join(root, "deploy/zarf/README.md"), encoding: "UTF-8")
external = readme[/^## Declared external dependencies\n(.*?)(?=^## )/m, 1]
abort "Zarf README must declare the package's external dependencies" unless external
workspace_default = File.read(File.join(root, "internal/devworkspace/domain/pod.go"))[/request\.Image = "([^"]+)"/, 1]
abort "workspace editor image default moved; update the external dependency declaration" unless workspace_default
{"PostgreSQL" => "PACKAGING_POSTGRES_SECRET", "Temporal" => "PACKAGING_TEMPORAL_ADDRESS", "Workspace editor image" => "`#{workspace_default}`"}.each do |dependency, detail|
  row = external.lines.find { |line| line.start_with?("| #{dependency} |") }
  abort "Zarf README must declare #{dependency} as an external dependency naming #{detail}" unless row&.include?(detail)
end
# The workspace image is external and unpackaged, so no packaged workload may
# be configured to start it: an air-gapped cluster could never pull it.
Dir[File.join(root, "deploy/zarf/manifests/*.yaml")].each do |file|
  abort "#{File.basename(file)} enables workspace Pods, whose image is not in the package" if File.read(file).include?("DEVWORKSPACE_")
end

namespaces = manifests.filter_map { |item| item.dig("metadata", "namespace") }.uniq
abort "Zarf runtime escaped platform-system" unless namespaces == ["platform-system"]
created_namespaces = manifests.select { |item| item["kind"] == "Namespace" }.map { |item| item.dig("metadata", "name") }
abort "Zarf must create only its runtime namespace" unless created_namespaces == ["platform-system"]
abort "Zarf package must not enable insecure identity" if manifest_text.match?(/ALLOW_INSECURE_.*true/)
# The match above is by prefix and was written for identity opt-ins. The model
# plaintext opt-in (#807) is the one insecure variable this package may name,
# and only as an operator's deploy-time decision that defaults to off: never a
# literal in the manifest. Anything else with ALLOW_INSECURE in its name fails.
insecure_names = manifest_text.scan(/name:\s*([A-Z0-9_]*ALLOW_INSECURE[A-Z0-9_]*)/).flatten.uniq
abort "Zarf package names an insecure opt-in other than the model plaintext one: #{insecure_names.join(', ')}" unless
  insecure_names == ["CONDUIT_MODEL_ALLOW_INSECURE_HTTP"]
%w[OPERATOR_OIDC_ISSUER OPERATOR_OIDC_CLIENT_ID OPERATOR_OIDC_USERNAME_CLAIM OPERATOR_OIDC_GROUPS_CLAIM OPERATOR_OIDC_CA_FILE].each do |name|
  abort "Hub must accept #{name} from a Zarf variable" unless manifest_text.include?("name: #{name}") && manifest_text.include?("###ZARF_VAR_#{name}###")
  variable = variables.find { |item| item["name"] == name }
  abort "#{name} must default to empty so a plain install does not require Keycloak" unless variable && variable["default"] == ""
end
# Browser sign-in. The two settings that would make a Hub with no issuer refuse
# to start default to empty; the client secret reaches the Hub only as a file
# from an optional Secret, never as a variable's value.
%w[OPERATOR_OIDC_CLIENT_SECRET_FILE OPERATOR_SESSION_MAX_LIFETIME].each do |name|
  abort "Hub must accept #{name} from a Zarf variable" unless manifest_text.include?("name: #{name}") && manifest_text.include?("###ZARF_VAR_#{name}###")
  variable = variables.find { |item| item["name"] == name }
  abort "#{name} must default to empty so a plain install does not require Keycloak" unless variable && variable["default"] == ""
  abort "#{name} must carry a pattern" if variable["pattern"].to_s.empty?
end
abort "Hub must be told the console's public address for sign-in" unless
  manifest_text.include?('{ name: CONSOLE_PUBLIC_URL, value: "###ZARF_VAR_CONSOLE_PUBLIC_URL###" }')
abort "the OIDC client secret must never be a variable's value" if
  variables.any? { |item| item["name"].match?(/CLIENT_SECRET(_VALUE)?\z/) && item["name"] != "OPERATOR_OIDC_CLIENT_SECRET" } ||
  manifest_text.match?(/name:\s*OPERATOR_OIDC_CLIENT_SECRET\s*,/)
abort "Zarf package must require the persistent Bundle signing key" unless manifest_text.include?("BUNDLE_SIGNING_KEY_FILE")
abort "Zarf package must use static mTLS in the disconnected baseline" unless %w[
  AGENT_CLIENT_CA_FILE TLS_CERT_FILE TLS_KEY_FILE
  CONTROL_PLANE_CA_FILE CONTROL_PLANE_CLIENT_CERT_FILE CONTROL_PLANE_CLIENT_KEY_FILE
].all? { |name| manifest_text.include?(name) }
abort "Zarf package must not contain credential bytes" if manifest_text.match?(/BEGIN (?:PRIVATE KEY|CERTIFICATE)/)
abort "Zarf runtime must not require a root bootstrap container" if manifest_text.include?("initContainers")
# REPOSITORY_KIND selects the profile, and the settings that must move with
# it are derived, not typed (#828). Unquoted on purpose: replicas must be an
# integer and strategy a mapping once Zarf has substituted its text. The
# resolved profiles are checked by verify-zarf-chart-equivalence.rb.
{"replicas" => "HUB_REPLICAS", "strategy" => "HUB_STRATEGY"}.each do |field, variable|
  abort "Hub #{field} must be derived from REPOSITORY_KIND as #{variable}" unless manifest_text.scan(/^  #{field}: (.*)$/).flatten.first == "###ZARF_VAR_" + variable + "###"
end
abort "Hub state volume must be derived from REPOSITORY_KIND" unless manifest_text.include?("\n        - name: state\n          ###ZARF_VAR_HUB_STATE_VOLUME" + "###\n")
# Planning sessions are process-local unless a Temporal frontend is set, and
# the Hub refuses more than one replica without one. So the Hub must be told
# the replica count this manifest really runs, never a literal: a hard-coded
# "1" beside two replicas silences the Hub's own guard.
hub_env_text = manifest_text.scan(/^\s+- \{ name: ([A-Z_]+), value: (.*?) \}$/).to_h
abort "Hub must be told the derived replica count, not a literal" unless hub_env_text["HUB_REPLICA_COUNT"] == '"###ZARF_VAR_HUB_REPLICAS' + '###"'
{"ADDRESS" => ["", "^$|^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?:[0-9]{1,5}$"], "NAMESPACE" => ["default", nil], "TASK_QUEUE" => ["planning-sessions", nil]}.each do |suffix, (default, pattern)|
  abort "Hub must accept CONDUIT_TEMPORAL_#{suffix} from PLANNING_TEMPORAL_#{suffix}" unless hub_env_text["CONDUIT_TEMPORAL_#{suffix}"] == %("###ZARF_VAR_PLANNING_TEMPORAL_#{suffix}) + '###"'
  variable = variables.find { |item| item["name"] == "PLANNING_TEMPORAL_#{suffix}" }
  abort "PLANNING_TEMPORAL_#{suffix} must default to #{default.inspect}" unless variable && variable["default"] == default && (pattern.nil? || variable["pattern"] == pattern)
end
abort "Hub must treat the planning frontend as shared between replicas" unless hub_env_text["CONDUIT_TEMPORAL_SHARED"] == '"true"'
budget = manifests.find { |item| item["kind"] == "PodDisruptionBudget" }
abort "Zarf package must carry the Hub PodDisruptionBudget" unless
  budget&.dig("metadata", "name") == "platform-delivery" && budget.dig("spec", "maxUnavailable") == 1 &&
  budget.dig("spec", "selector", "matchLabels") == ({"app.kubernetes.io/name" => "platform-delivery"})
deployments = manifests.select { |item| item["kind"] == "Deployment" }
abort "Zarf runtime must contain two Deployments" unless deployments.length == 2
deployments.each do |deployment|
  pod_security = deployment.dig("spec", "template", "spec", "securityContext") || {}
  abort "Zarf runtime Pod must use the non-root runtime group" unless pod_security["runAsUser"] == 65_532 && pod_security["runAsGroup"] == 65_532
  deployment.dig("spec", "template", "spec", "containers").each do |container|
    abort "Zarf runtime container must drop every capability" unless container.dig("securityContext", "capabilities", "drop") == ["ALL"]
  end
end
hub = deployments.find { |deployment| deployment.dig("metadata", "name") == "platform-delivery" }
hub_container = hub&.dig("spec", "template", "spec", "containers")&.find { |container| container["name"] == "delivery-control-plane" }
%w[startupProbe readinessProbe livenessProbe].each do |probe|
  abort "Zarf Hub #{probe} must use HTTPS" unless hub_container&.dig(probe, "httpGet", "scheme") == "HTTPS"
end
abort "Zarf runtime namespace must opt into labeled image mutation" unless manifests.find { |item| item["kind"] == "Namespace" }&.dig("metadata", "labels", "zarf.dev/agent") == "mutate"
pvc = manifests.find { |item| item["kind"] == "PersistentVolumeClaim" }
abort "Zarf removal must retain durable Hub state" unless pvc&.dig("metadata", "annotations", "helm.sh/resource-policy") == "keep"
health_checks = (definition.dig("components", 0, "actions", "onDeploy", "onSuccess") || []).filter_map { |action| action.dig("wait", "cluster") }
abort "Zarf package must health-check both runtime deployments" unless health_checks.map { |item| item["name"] }.sort == %w[platform-delivery platform-spoke-agent]
service = manifests.find { |item| item["kind"] == "Service" && item.dig("metadata", "name") == "platform-delivery" }
# Metrics in this package (#838). The Hub serves /metrics only when
# METRICS_ADDRESS is set and the repository is postgres, so both are deploy
# variables, both default to "off", and the address can only be the port the
# Service and container already name.
hub_env = hub_container.fetch("env").to_h { |item| [item["name"], item["value"]] }
{"REPOSITORY_KIND" => ["file", "^(file|postgres)$"], "METRICS_ADDRESS" => ["", "^(:9090)?$"]}.each do |name, (default, pattern)|
  abort "Hub must accept #{name} from a Zarf variable" unless hub_env[name] == "###ZARF_VAR_#{name}###"
  variable = variables.find { |item| item["name"] == name }
  abort "#{name} must default to #{default.inspect} and be constrained to #{pattern}" unless
    variable && variable["default"] == default && variable["pattern"] == pattern
end
# Request limits in this package (#842). Six deploy variables, all empty by
# default, which the Hub reads as "keep the built-in limit". No variable turns
# the limiter off, and the patterns admit only numbers.
%w[OPERATOR AGENT ANONYMOUS].each do |limited|
  {"RPS" => "^$|^[0-9]{1,7}(\\.[0-9]{1,3})?$", "BURST" => "^$|^[1-9][0-9]{0,6}$"}.each do |kind, pattern|
    name = "RATE_LIMIT_#{limited}_#{kind}"
    abort "Hub must accept #{name} from a Zarf variable" unless hub_env[name] == "###ZARF_VAR_#{name}###"
    variable = variables.find { |item| item["name"] == name }
    abort "#{name} must default to empty and be constrained to #{pattern}" unless
      variable && variable["default"] == "" && variable["pattern"] == pattern
    abort "#{name} pattern admits a non-number" if %w[off none -1 1e9 unlimited].any? { |value| value.match?(Regexp.new(pattern)) }
  end
end
abort "no Zarf variable may disable the request limiter" if
  variables.any? { |item| item["name"].match?(/RATE_LIMIT.*(DISABLE|ENABLE|OFF)/) }
# Alert rules and the dashboard (#839): an optional component, off by default,
# whose manifest is generated from deploy/observability and holds exactly a
# PrometheusRule and a dashboard ConfigMap. scripts/verify-observability-assets.rb
# holds the manifest to its sources; this holds the component to the manifest.
observability_component = definition.fetch("components").find { |item| item["name"] == "uds-observability" }
abort "uds-observability must be an optional component that is off by default" unless
  observability_component && observability_component["optional"] == true
abort "uds-observability must apply only the generated observability manifest" unless
  observability_component.fetch("manifests") == [{"name" => "observability", "namespace" => "platform-system", "files" => ["manifests/observability.yaml"]}]
observability_documents = YAML.load_stream(File.read(File.join(root, "deploy/zarf/manifests/observability.yaml"))).compact
abort "the observability manifest must hold one PrometheusRule and one dashboard ConfigMap in platform-system" unless
  observability_documents.map { |item| item["kind"] }.sort == %w[ConfigMap PrometheusRule] &&
    observability_documents.all? { |item| item.dig("metadata", "namespace") == "platform-system" } &&
    observability_documents.find { |item| item["kind"] == "ConfigMap" }.dig("metadata", "labels", "grafana_dashboard") == "1"
abort "the observability manifest must not carry a Zarf variable; it is generated" if
  File.read(File.join(root, "deploy/zarf/manifests/observability.yaml")).include?("###ZARF_")
# Model provider in this package (#807). Every variable defaults to "no model
# call", the key and CA are files from optional mounts whose only accepted
# paths are the ones mounted here, and no model command is configurable: the
# Hub image carries no model CLI.
{
  "CONDUIT_ASSISTANT_MODEL_NAME" => ["ASSISTANT_MODEL_NAME", "", "^\\S*$"],
  "CONDUIT_ASSISTANT_MODEL_TIMEOUT" => ["ASSISTANT_MODEL_TIMEOUT", "", "^$|^[1-9][0-9]*(ms|s|m)$"],
  "CONDUIT_PLANNING_MODEL_NAME" => ["PLANNING_MODEL_NAME", "", "^\\S*$"],
  "CONDUIT_PLANNING_MODEL_TIMEOUT" => ["PLANNING_MODEL_TIMEOUT", "", "^$|^[1-9][0-9]*(ms|s|m)$"],
  "CONDUIT_PACKAGING_MODEL_NAME" => ["PACKAGING_MODEL_NAME", "", "^\\S*$"],
  "CONDUIT_PACKAGING_MODEL_TIMEOUT" => ["PACKAGING_MODEL_TIMEOUT", "", "^$|^[1-9][0-9]*(ms|s|m)$"],
  "CONDUIT_APPGEN_MODEL_NAME" => ["APPGEN_MODEL_NAME", "", "^\\S*$"],
  "CONDUIT_APPGEN_MODEL_TIMEOUT" => ["APPGEN_MODEL_TIMEOUT", "", "^$|^[1-9][0-9]*(ms|s|m)$"],
  "CONDUIT_MODEL_BASE_URL" => ["MODEL_BASE_URL", "", "^$|^https?://[^\\s@?#]+$"],
  "CONDUIT_MODEL_NAME" => ["MODEL_NAME", "", "^\\S*$"],
  "CONDUIT_MODEL_TIMEOUT" => ["MODEL_TIMEOUT", "", "^$|^[1-9][0-9]*(ms|s|m)$"],
  "CONDUIT_MODEL_API_KEY_FILE" => ["MODEL_API_KEY_FILE", "", "^(/run/conduit-model/api-key)?$"],
  "CONDUIT_MODEL_CA_FILE" => ["MODEL_CA_FILE", "", "^(/run/conduit-model-ca/ca.pem)?$"],
  "CONDUIT_MODEL_ALLOW_INSECURE_HTTP" => ["MODEL_ALLOW_INSECURE_HTTP", "false", "^(true|false)$"]
}.each do |name, (variable_name, default, pattern)|
  abort "Hub must accept #{name} from the Zarf variable #{variable_name} and never a literal" unless hub_env[name] == "###ZARF_VAR_#{variable_name}###"
  variable = variables.find { |item| item["name"] == variable_name }
  abort "#{variable_name} must default to #{default.inspect} and be constrained to #{pattern}" unless
    variable && variable["default"] == default && variable["pattern"] == pattern
end
abort "Zarf package must not configure a model command" if manifest_text.include?("CONDUIT_MODEL_CMD")
abort "Zarf package must not carry the model API key as an environment value" if
  hub_container.fetch("env").any? { |item| item["name"].match?(/MODEL.*KEY/) && item["name"] != "CONDUIT_MODEL_API_KEY_FILE" }
hub_volumes = (hub.dig("spec", "template", "spec", "volumes") || []).to_h { |item| [item["name"], item] }
abort "model API key must be an optional Secret key mounted as a file" unless
  hub_volumes.dig("conduit-model", "secret") == {
    "secretName" => "###ZARF_VAR_MODEL_API_KEY_SECRET###", "optional" => true, "items" => [{"key" => "api-key", "path" => "api-key"}]
  }
abort "model CA must be an optional ConfigMap key mounted as a file" unless
  hub_volumes.dig("conduit-model-ca", "configMap") == {
    "name" => "###ZARF_VAR_MODEL_CA_CONFIGMAP###", "optional" => true, "items" => [{"key" => "ca.pem", "path" => "ca.pem"}]
  }
abort "model credential mounts must be read-only where the file variables point" unless
  %w[conduit-model conduit-model-ca].all? do |name|
    hub_container.fetch("volumeMounts").include?({"name" => name, "mountPath" => "/run/#{name}", "readOnly" => true})
  end
abort "Hub must read the PostgreSQL DSN from its mounted Secret" unless hub_env["POSTGRES_DSN_FILE"] == "/run/conduit-database/dsn"
database = (hub.dig("spec", "template", "spec", "volumes") || []).find { |item| item["name"] == "database" }
abort "Hub DSN Secret must be optional, so the file-repository default starts without it" unless
  database&.dig("secret") == {"secretName" => "###ZARF_VAR_PACKAGING_POSTGRES_SECRET###", "optional" => true, "items" => [{"key" => "dsn", "path" => "dsn"}]}
abort "Hub DSN Secret must be mounted read-only where POSTGRES_DSN_FILE points" unless
  hub_container.fetch("volumeMounts").include?({"name" => "database", "mountPath" => "/run/conduit-database", "readOnly" => true})
abort "Hub Service must label itself for the UDS monitor" unless service&.dig("metadata", "labels", "app.kubernetes.io/name") == "platform-delivery"
abort "Hub Service must name the metrics port" unless service&.dig("spec", "ports")&.any? { |port| port["name"] == "metrics" && port["port"] == 9090 }
abort "Hub container must declare the metrics port" unless hub_container&.dig("ports")&.any? { |port| port["name"] == "metrics" && port["containerPort"] == 9090 }
operator_sso = (hub.dig("spec", "template", "spec", "volumes") || []).find { |item| item["name"] == "operator-sso" }
abort "Hub must mount the OIDC client secret as an optional Secret, by key" unless
  operator_sso&.dig("secret", "secretName") == "###ZARF_VAR_OPERATOR_OIDC_CLIENT_SECRET###" && operator_sso.dig("secret", "optional") &&
  operator_sso.dig("secret", "items") == [{ "key" => "client-secret", "path" => "client-secret" }]
operator_sso_mount = (hub.dig("spec", "template", "spec", "containers", 0, "volumeMounts") || []).find { |item| item["name"] == "operator-sso" }
abort "Hub must mount the OIDC client secret read-only where OPERATOR_OIDC_CLIENT_SECRET_FILE points" unless
  operator_sso_mount == { "name" => "operator-sso", "mountPath" => "/run/conduit-sso", "readOnly" => true }
oidc_ca = (hub.dig("spec", "template", "spec", "volumes") || []).find { |item| item["name"] == "oidc-ca" }
abort "Hub must mount the issuer CA bundle as an optional ConfigMap" unless oidc_ca&.dig("configMap", "name") == "uds-trust-bundle" && oidc_ca.dig("configMap", "optional")

abort "UDS Package API must be pinned" unless uds_package["apiVersion"] == "uds.dev/v1alpha1" && uds_package["kind"] == "Package"
abort "UDS Package must live in platform-system" unless uds_package.dig("metadata", "namespace") == "platform-system"
expose = uds_package.dig("spec", "network", "expose") || []
hub_expose = expose.find { |item| item["gateway"] == "tenant" && item["service"] == "platform-delivery" && item["port"] == 8081 }
abort "UDS Package must expose the Hub on the tenant gateway" unless hub_expose
abort "UDS Package must use sidecar mesh mode when ambient is off" unless uds_package.dig("spec", "network", "serviceMesh", "mode") == "sidecar"
abort "UDS Package must set the portal tile title on the Hub expose entry" unless hub_expose.dig("annotations", "uds.dev/title") == "Conduit"
abort "Zarf package must set the portal tile title" unless definition.dig("metadata", "annotations", "dev.uds.title") == "Conduit"
allows = uds_package.dig("spec", "network", "allow") || []
required_allows = [
  ["Ingress", "istio-tenant-gateway", 8081],
  ["Ingress", "platform-system", 8081],
  ["Egress", "###ZARF_VAR_POSTGRES_NAMESPACE###", 5432],
  ["Egress", "platform-system", 8081]
]
required_allows.each do |direction, namespace, port|
  abort "UDS Package missing #{direction} allow to #{namespace}:#{port}" unless allows.any? { |item| item["direction"] == direction && item["remoteNamespace"] == namespace && item["port"] == port }
end
abort "UDS Package must allow the Spoke to call the Kubernetes API" unless allows.any? { |item| item["direction"] == "Egress" && item["remoteGenerated"] == "KubeAPI" }
# Egress the runtime needs and UDS Core denies by default (#829). Each is
# matched whole, source selector included: an allow that names the right
# destination from the wrong workload grants nothing where it is needed, and
# reads exactly like a grant.
required_egress = {
  "the Hub to reach the OIDC issuer through the tenant gateway" => {
    "selector" => {"app.kubernetes.io/name" => "platform-delivery"},
    "remoteNamespace" => "istio-tenant-gateway", "remoteSelector" => {"app" => "tenant-ingressgateway"}, "ports" => [443, 8443]
  },
  "the Hub to reach the in-cluster OIDC issuer" => {
    "selector" => {"app.kubernetes.io/name" => "platform-delivery"},
    "remoteNamespace" => "###ZARF_VAR_OIDC_ISSUER_NAMESPACE###",
    "remoteSelector" => {"app.kubernetes.io/name" => "###ZARF_VAR_OIDC_ISSUER_NAME_LABEL###"}, "port" => 8080
  },
  "the Spoke to pull charts from the OCI registry" => {
    "selector" => {"app.kubernetes.io/name" => "platform-spoke-agent"},
    "remoteNamespace" => "###ZARF_VAR_CHART_REGISTRY_NAMESPACE###",
    "remoteSelector" => {"###ZARF_VAR_CHART_REGISTRY_LABEL_KEY###" => "###ZARF_VAR_CHART_REGISTRY_LABEL_VALUE###"}, "ports" => [443, 5000]
  },
  "the Hub to reach the Temporal frontend that holds planning sessions" => {
    "selector" => {"app.kubernetes.io/name" => "platform-delivery"},
    "remoteNamespace" => "###ZARF_VAR_TEMPORAL_NAMESPACE###",
    "remoteSelector" => {"app.kubernetes.io/name" => "###ZARF_VAR_TEMPORAL_NAME_LABEL###"}, "port" => 7233
  },
  "the packaging worker to reach Temporal" => {
    "selector" => {"app.kubernetes.io/name" => "platform-packaging-worker"},
    "remoteNamespace" => "###ZARF_VAR_TEMPORAL_NAMESPACE###",
    "remoteSelector" => {"app.kubernetes.io/name" => "###ZARF_VAR_TEMPORAL_NAME_LABEL###"}, "port" => 7233
  },
  # Matched whole, so a port, a remoteHost or a wider selector added to this
  # entry fails here and has to be argued for.
  "the Hub to reach an in-cluster model endpoint" => {
    "selector" => {"app.kubernetes.io/name" => "platform-delivery"},
    "remoteNamespace" => "###ZARF_VAR_MODEL_SERVICE_NAMESPACE###",
    "remoteSelector" => {"app.kubernetes.io/name" => "###ZARF_VAR_MODEL_SERVICE_NAME_LABEL###"}
  }
}
required_egress.each do |purpose, expected|
  abort "UDS Package must allow #{purpose}" unless
    allows.any? { |item| item["direction"] == "Egress" && item.reject { |key, _| %w[direction description].include?(key) } == expected }
end
# A placeholder with no declared variable is deployed as its literal text: a
# NetworkPolicy naming the namespace "###ZARF_VAR_...###" selects nothing.
declared_variables = variables.map { |item| item["name"] }
undeclared = uds_text.scan(/###ZARF_VAR_([A-Z0-9_]+)###/).flatten.uniq - declared_variables
abort "UDS Package uses undeclared Zarf variables: #{undeclared.join(', ')}" unless undeclared.empty?
# The model egress is granted only when configured. A Zarf variable cannot
# drop a list entry, so "not configured" is a default that names no namespace
# and no workload: the rendered policy then selects nothing.
%w[MODEL_SERVICE_NAMESPACE MODEL_SERVICE_NAME_LABEL].each do |name|
  variable = variables.find { |item| item["name"] == name }
  abort "#{name} must default to the sentinel that selects nothing, so no model egress is granted until an operator sets it" unless
    variable && variable["default"] == "conduit-model-unconfigured"
end
abort "only the Hub may be granted egress to the model endpoint" unless
  allows.count { |item| item.to_s.include?("ZARF_VAR_MODEL_SERVICE_") } == 1
%w[OIDC_ISSUER_NAMESPACE OIDC_ISSUER_NAME_LABEL CHART_REGISTRY_NAMESPACE CHART_REGISTRY_LABEL_KEY CHART_REGISTRY_LABEL_VALUE TEMPORAL_NAMESPACE TEMPORAL_NAME_LABEL MODEL_SERVICE_NAMESPACE MODEL_SERVICE_NAME_LABEL].each do |name|
  variable = variables.find { |item| item["name"] == name }
  abort "#{name} must have a non-empty default and a pattern, so an unset variable cannot render an empty selector" unless
    variable && !variable["default"].to_s.empty? && !variable["pattern"].to_s.empty?
end
sso = uds_package.dig("spec", "sso") || []
client = sso.find { |item| item["clientId"] == "conduit-hub" }
abort "UDS Package must register the Hub SSO client" unless client
# The Hub is the OAuth client (docs/operations/operator-sign-in.md). Its one
# callback is registered exactly, with no wildcard, and the secret UDS Core
# generates is written under the key the Hub's mount reads.
abort "UDS Package must register exactly the Hub's sign-in callback" unless client["redirectUris"] == ["###ZARF_VAR_CONSOLE_PUBLIC_URL###/v1/auth/callback"]
abort "UDS Package must register a confidential client with the standard flow" unless client["publicClient"] == false && client["standardFlowEnabled"] == true
abort "UDS Package must write the client secret under the key the Hub mounts" unless
  client.dig("secretConfig", "name") == "conduit-hub-sso" && client.dig("secretConfig", "template") == { "client-secret" => "clientField(secret)" }
abort "UDS Package must not put authservice in front of the Agent mTLS port" if client["enableAuthserviceSelector"]
groups_mapper = (client["protocolMappers"] || []).find { |item| item["protocolMapper"] == "oidc-group-membership-mapper" && item.dig("config", "claim.name") == "groups" }
abort "UDS Package must map Keycloak groups into the token" unless groups_mapper
abort "UDS Package must write the issuer CA bundle for operator OIDC" unless uds_package.dig("spec", "caBundle", "configMap", "name") == "uds-trust-bundle"
# The default Hub is file-backed and serves no /metrics. A monitor registered
# against it is a target that is down for the life of the deployment, so the
# monitor lives only in the variant an operator selects together with a
# working metrics configuration.
abort "default UDS Package must not register a monitor the file-backed Hub cannot serve" if uds_package["spec"].key?("monitor")
metrics_package = YAML.safe_load(metrics_text)
monitor = metrics_package.dig("spec", "monitor") || []
abort "UDS metrics Package must declare exactly the Hub ServiceMonitor" unless monitor == [{
  "description" => "Hub metrics", "kind" => "ServiceMonitor",
  "selector" => {"app.kubernetes.io/name" => "platform-delivery"},
  "portName" => "metrics", "targetPort" => 9090, "path" => "/metrics"
}]
without_monitor = metrics_package.merge("spec" => metrics_package["spec"].reject { |key, _| key == "monitor" })
abort "UDS metrics Package must equal the default Package apart from its monitor" unless without_monitor == uds_package
undeclared_metrics = metrics_text.scan(/###ZARF_VAR_([A-Z0-9_]+)###/).flatten.uniq - declared_variables
abort "UDS metrics Package uses undeclared Zarf variables: #{undeclared_metrics.join(', ')}" unless undeclared_metrics.empty?

abort "native UDS bundle must track VERSION" unless bundle_text[/^  version = "([^"]+)" # x-release-please-version$/, 1] == version
abort "native UDS bundle must carry one runtime package" unless bundle_text.scan(/^package "([^"]+)" \{/).flatten == ["platform_runtime"]
[
  'bundle_api_version = "uds.dev/v1alpha1"', 'source = "./zarf-package-platform-runtime-${sys.arch}-${local.version}.tar.zst"',
  'optional_components = ["uds"]', 'values_files = ["runtime-values.yaml"]',
  'certificate_identity = "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v${local.version}"',
  'certificate_oidc_issuer = "https://token.actions.githubusercontent.com"'
].each { |binding| abort "native UDS bundle lost binding: #{binding}" unless bundle_text.include?(binding) }
abort "native UDS package trust must not be disabled" if bundle_text.match?(/verify\s*=\s*false|skip.signature/i)
ci_workflow = File.read(File.join(root, ".github/workflows/platform-ci.yml"), encoding: "UTF-8")
abort "Trunk gate must install the pinned UDS CLI" unless ci_workflow.include?("uds-cli_v0.41.0_Linux_amd64") && ci_workflow.include?("31cd710a3f1154f81767a5268f2c887d9e33fe10b81e45ee4183c810dd5ad308  /tmp/uds")
uds_config_text = File.read(File.join(zarf_directory, "config.uds.hcl"), encoding: "UTF-8")
body = uds_config_text.sub(/\A(?:#[^\n]*\n)*/, "")
abort "native bundle example must contain only scalar variables" unless body.match?(/\Avariables = \{\n(?:  "[^"\n]+" = [^\n]+\n)*\}\n\z/)
entries = body.scan(/^  ("[^"\n]+") = ([^\n]+)$/).map { |key, value| [JSON.parse(key), JSON.parse(value)] }
abort "native bundle example repeats a variable" unless entries.map(&:first).uniq.length == entries.length
configured = entries.to_h
unknown = configured.keys - declared_variables
abort "native bundle example sets undeclared values: #{unknown.join(', ')}" unless unknown.empty?
required_config = %w[ENVIRONMENT_ID AGENT_ID BOOTSTRAP_ADMINISTRATORS REPOSITORY_KIND PACKAGING_POSTGRES_SECRET PLANNING_TEMPORAL_ADDRESS
                     PACKAGING_TEMPORAL_ADDRESS PACKAGING_TEMPORAL_NAMESPACE PACKAGING_TEMPORAL_TASK_QUEUE
                     OPERATOR_OIDC_ISSUER OPERATOR_OIDC_CLIENT_ID OPERATOR_OIDC_USERNAME_CLAIM OPERATOR_OIDC_GROUPS_CLAIM OPERATOR_OIDC_CA_FILE]
without_default = variables.reject { |item| item.key?("default") }.map { |item| item["name"] }
missing = (required_config | without_default) - configured.keys
abort "config.uds.hcl must carry the deployment variables: #{missing.join(', ')}" unless missing.empty?
abort "config.uds.hcl must select the production profile" unless configured["REPOSITORY_KIND"] == "postgres"
abort "config.uds.hcl must name the planning frontend the second Hub replica depends on" if configured["PLANNING_TEMPORAL_ADDRESS"].to_s.empty?
configured.each do |name, value|
  abort "config.uds.hcl #{name} must be a plain string" unless value.is_a?(String)
  pattern = variables.find { |item| item["name"] == name }["pattern"]
  abort "config.uds.hcl #{name} does not match the pattern the package enforces at deploy" if pattern && !value.match?(Regexp.new(pattern))
end
# No secret may sit in a file that is read as plain text and reaches Pod specs.
abort "config.uds.hcl must not carry credential material" if
  uds_config_text.match?(/BEGIN [A-Z ]*(?:PRIVATE KEY|CERTIFICATE)|:\/\/[^\s\/:@]+:[^\s\/@]+@|(?:password|passwd|token|api[_-]?key)\s*[:=]\s*\S/i)

# Publication of the signed package to the OCI registry (#832). It is its own
# job so `packages: write` is all it holds, it runs only after the job that
# signed and verified the packages, and what it trusts is the read-back from
# the registry rather than the file it pushed.
images_workflow_text = File.read(File.join(root, ".github/workflows/platform-images.yml"), encoding: "UTF-8")
publish_job = YAML.safe_load(images_workflow_text, aliases: true).dig("jobs", "publish-zarf-package")
abort "publication workflow must publish the signed Zarf packages in their own job" unless publish_job
abort "Zarf package publication may hold only packages: write" unless publish_job["permissions"] == {"contents" => "read", "packages" => "write"}
abort "Zarf package publication must follow signing and verification" unless Array(publish_job["needs"]) == ["manifest"]
abort "Zarf package publication must be limited to release tags" unless publish_job["if"] == "startsWith(github.ref, 'refs/tags/v')"
publish_run = publish_job.fetch("steps").map { |step| step["run"].to_s }.join("\n")
abort "Zarf package publication must use the checksum-pinned Zarf" unless
  publish_run.include?("zarf_v1.0.0_Linux_amd64") && publish_run.include?("c58cf0241796525d9df9a449a5a2218743efa446b540a996f355d71349852857  /tmp/zarf' | sha256sum --check")
abort "Zarf packages must be published to the repository-linked packages namespace" unless
  publish_run.include?('repository="oci://ghcr.io/${GITHUB_REPOSITORY}/packages"')
abort "Zarf package publication must cover both architectures" unless publish_run.scan("for architecture in amd64 arm64; do").length == 2
keyless = ['--certificate-identity "${identity}"', '--certificate-oidc-issuer "${issuer}"']
publish_command = publish_run[/\/tmp\/zarf package publish .*?--confirm/m].to_s
abort "Zarf package publication must verify the signature before pushing" unless
  publish_command.include?("--verify=always") && keyless.all? { |flag| publish_command.include?(flag) }
readback = publish_run[/\/tmp\/zarf package verify "\$\{repository\}\/platform-runtime:\$\{version\}".*?issuer\}"/m].to_s
abort "Zarf package publication must verify what the registry serves, per architecture" unless
  readback.include?('--architecture "${architecture}"') && keyless.all? { |flag| readback.include?(flag) } &&
  publish_run.index(readback) > publish_run.index(publish_command)
abort "Zarf package publication must bind the signer to this workflow run" unless
  publish_run.include?('identity="${GITHUB_SERVER_URL}/${GITHUB_WORKFLOW_REF}"') && publish_run.include?('issuer="https://token.actions.githubusercontent.com"')
abort "Zarf package publication must check each package against its recorded checksum" unless publish_run.include?('sha256sum --check "${package}.sha256"')
# The lifecycle lane is a deployment rehearsal: dispatch only (ADR 0006), and
# with no authority to write anything.
lifecycle_workflow = YAML.safe_load(File.read(File.join(root, ".github/workflows/platform-uds-lifecycle.yml"), encoding: "UTF-8"), aliases: true)
lifecycle_triggers = lifecycle_workflow["on"] || lifecycle_workflow[true]
abort "UDS package lifecycle must run only on explicit dispatch" unless lifecycle_triggers.is_a?(Hash) && lifecycle_triggers.keys == ["workflow_dispatch"]
lifecycle_permissions = [lifecycle_workflow["permissions"]] + lifecycle_workflow.fetch("jobs").values.map { |job| job["permissions"] }
abort "UDS package lifecycle must hold no write permission" unless
  lifecycle_permissions.compact.all? { |permissions| permissions.is_a?(Hash) && permissions.values.all? { |value| %w[read none].include?(value) } } &&
  lifecycle_workflow["permissions"] == {"contents" => "read"}
lifecycle_run = lifecycle_workflow.fetch("jobs").values.flat_map { |job| job.fetch("steps") }.map { |step| step["run"].to_s }.join("\n")
abort "UDS package lifecycle must run the lifecycle script and keep its verdict" unless
  lifecycle_run.include?("set -o pipefail") && lifecycle_run.include?("./scripts/test-zarf-package-lifecycle.sh")
lifecycle_script = File.read(File.join(root, "scripts/test-zarf-package-lifecycle.sh"), encoding: "UTF-8")
deploy_function = File.read(File.join(root, "scripts/lib/zarf-lifecycle-deploy.sh"), encoding: "UTF-8")
abort "lifecycle deployments must verify the signature against the release's own identity" unless
  ["--verify=always", '--certificate-identity "$(identity "${release}")"', '--certificate-oidc-issuer "${issuer}"'].all? { |flag| deploy_function.include?(flag) }
abort "lifecycle lane must assert the Hub state claim survives upgrade and removal" unless
  lifecycle_script.scan('[[ "$(claim_uid)" == "${installed_uid}" ]]').length == 2 && lifecycle_script.include?("package remove platform-runtime --confirm")

example = File.join(root, "internal/packagingworkflow/fixtures/example-package")
example_definition = YAML.safe_load(File.read(File.join(example, "zarf.yaml")))
abort "generated package API must be pinned" unless example_definition["apiVersion"] == "zarf.dev/v1beta1"
abort "generated package must install the UDS Package manifest" unless example_definition.dig("components", 0, "manifests", 0, "files") == ["uds-package.yaml"]
package_text = File.read(File.join(example, "uds-package.yaml"))
abort "generated package CR must be uds.dev/v1alpha1 Package" unless package_text.include?("apiVersion: uds.dev/v1alpha1") && package_text.include?("kind: Package")
%w[sso: allow: monitor:].each do |section|
  abort "generated package CR missing #{section}" unless package_text.include?(section)
end

zarf = ENV["ZARF_BIN"]
if ENV.key?("ZARF_BIN")
  abort "ZARF_BIN must name a Zarf executable" if zarf.to_s.empty?
  Dir.mktmpdir("conduit-runtime-lint-") do |work|
    output, error, result = Open3.capture3({"ZARF_BIN" => zarf}, "bash", File.join(root, "scripts/render-zarf-runtime.sh"), File.join(work, "package"), "0.0.0",
      "ghcr.io/example/platform-hub:0.0.0@sha256:#{'a' * 64}", "ghcr.io/example/platform-spoke-agent:0.0.0@sha256:#{'b' * 64}")
    abort "Zarf template compilation failed: #{output}#{error}" unless result.success?
    [[File.join(work, "package/zarf.gen.yaml"), "Zarf CLI"], [example, "generated package"]].each do |path, label|
      output, error, result = Open3.capture3(zarf, "dev", "lint", path, "--architecture", "amd64")
      abort "#{label} lint failed: #{output}#{error}" unless result.success?
      abort "#{label} lint emitted a deprecation warning: #{output}#{error}" if (output + error).match?(/deprecat/i)
    end
  end
end

puts "Zarf package structure and source-binding contract verified; native lint checked when ZARF_BIN is supplied"
