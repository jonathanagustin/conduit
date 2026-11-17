#!/usr/bin/env ruby
# frozen_string_literal: true

require "yaml"
require_relative "lib/safe_yaml"

root = File.expand_path("..", __dir__)
definition_path = File.join(root, "deploy/zarf/zarf.yaml")
manifest_path = File.join(root, "deploy/zarf/manifests/runtime.yaml")
uds_path = File.join(root, "deploy/zarf/manifests/uds-package.yaml")
bundle_path = File.join(root, "deploy/zarf/uds-bundle.yaml")
definition_text = File.read(definition_path)
manifest_text = File.read(manifest_path)
uds_text = File.read(uds_path)
definition = YAML.safe_load(definition_text)
manifests = ConduitSafeYAML.stream(manifest_text)
uds_package = YAML.safe_load(uds_text)
bundle = YAML.safe_load(File.read(bundle_path))
version = File.read(File.join(root, "VERSION")).strip

abort "Zarf package API must be pinned" unless definition["apiVersion"] == "zarf.dev/v1alpha1"
abort "Zarf package version must come from the release" unless definition.dig("metadata", "version") == "###ZARF_PKG_TMPL_PLATFORM_VERSION###"
abort "Zarf package components drifted" unless definition.fetch("components").map { |item| [item["name"], item["required"], item["default"]] } == [["runtime", true, nil], ["packaging-worker", false, nil], ["uds", false, false], ["uds-metrics", false, false]]
worker = definition.fetch("components").find { |item| item["name"] == "packaging-worker" }
abort "optional packaging worker must reuse the Hub image" unless worker&.dig("images") == ["###ZARF_PKG_TMPL_HUB_IMAGE###"]
abort "optional packaging worker must not be required" if worker["required"]
uds_component = definition.fetch("components").find { |item| item["name"] == "uds" }
abort "UDS component must stay optional and unselected by a plain install" if uds_component["required"] || uds_component["default"]
abort "UDS component must not carry images" if uds_component["images"]
abort "UDS component must apply only the Package CR" unless uds_component.dig("manifests", 0, "files") == ["manifests/uds-package.yaml"]
uds_metrics_component = definition.fetch("components").find { |item| item["name"] == "uds-metrics" }
abort "UDS metrics component must stay optional and unselected by a plain install" if uds_metrics_component["required"] || uds_metrics_component["default"]
abort "UDS metrics component must not carry images" if uds_metrics_component["images"]
abort "UDS metrics component must apply only the Package CR that carries the monitor" unless
  uds_metrics_component["manifests"].map { |item| item["files"] } == [["manifests/uds-package-metrics.yaml"]]
runtime_files = definition.dig("components", 0, "manifests", 0, "files")
abort "runtime component must not apply the UDS Package CR" if runtime_files.any? { |file| file.include?("uds-package") }

images = definition.dig("components", 0, "images")
abort "Zarf package must contain exactly Hub and Agent images" unless images&.length == 2
abort "Hub image must be supplied by the registry descriptor" unless images.include?("###ZARF_PKG_TMPL_HUB_IMAGE###")
abort "Agent image must be supplied by the registry descriptor" unless images.include?("###ZARF_PKG_TMPL_SPOKE_AGENT_IMAGE###")
images.grep_v(/###ZARF_PKG_TMPL_/).each do |image|
  abort "Zarf image is not pinned by digest: #{image}" unless image.match?(/:[^@]+@sha256:[a-f0-9]{64}\z/)
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
  variable = definition.fetch("variables").find { |item| item["name"] == name }
  abort "#{name} must default to empty so a plain install does not require Keycloak" unless variable && variable["default"] == ""
end
abort "Zarf package must require the persistent Bundle signing key" unless manifest_text.include?("BUNDLE_SIGNING_KEY_FILE")
abort "Zarf package must use static mTLS in the disconnected baseline" unless %w[
  AGENT_CLIENT_CA_FILE TLS_CERT_FILE TLS_KEY_FILE
  CONTROL_PLANE_CA_FILE CONTROL_PLANE_CLIENT_CERT_FILE CONTROL_PLANE_CLIENT_KEY_FILE
].all? { |name| manifest_text.include?(name) }
abort "Zarf package must not contain credential bytes" if manifest_text.match?(/BEGIN (?:PRIVATE KEY|CERTIFICATE)/)
abort "Zarf runtime must not require a root bootstrap container" if manifest_text.include?("initContainers")
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
health_checks = definition.dig("components", 0, "healthChecks") || []
abort "Zarf package must health-check both runtime deployments" unless health_checks.map { |item| item["name"] }.sort == %w[platform-delivery platform-spoke-agent]
service = manifests.find { |item| item["kind"] == "Service" && item.dig("metadata", "name") == "platform-delivery" }
# Metrics in this package (#838). The Hub serves /metrics only when
# METRICS_ADDRESS is set and the repository is postgres, so both are deploy
# variables, both default to "off", and the address can only be the port the
# Service and container already name.
hub_env = hub_container.fetch("env").to_h { |item| [item["name"], item["value"]] }
{"REPOSITORY_KIND" => ["file", "^(file|postgres)$"], "METRICS_ADDRESS" => ["", "^(:9090)?$"]}.each do |name, (default, pattern)|
  abort "Hub must accept #{name} from a Zarf variable" unless hub_env[name] == "###ZARF_VAR_#{name}###"
  variable = definition.fetch("variables").find { |item| item["name"] == name }
  abort "#{name} must default to #{default.inspect} and be constrained to #{pattern}" unless
    variable && variable["default"] == default && variable["pattern"] == pattern
end
# Model provider in this package (#807). Every variable defaults to "no model
# call", the key and CA are files from optional mounts whose only accepted
# paths are the ones mounted here, and no model command is configurable: the
# Hub image carries no model CLI.
{
  "CONDUIT_MODEL_BASE_URL" => ["MODEL_BASE_URL", "", "^$|^https?://[^\\s@?#]+$"],
  "CONDUIT_MODEL_NAME" => ["MODEL_NAME", "", "^\\S*$"],
  "CONDUIT_MODEL_TIMEOUT" => ["MODEL_TIMEOUT", "", "^$|^[1-9][0-9]*(ms|s|m)$"],
  "CONDUIT_MODEL_API_KEY_FILE" => ["MODEL_API_KEY_FILE", "", "^(/run/conduit-model/api-key)?$"],
  "CONDUIT_MODEL_CA_FILE" => ["MODEL_CA_FILE", "", "^(/run/conduit-model-ca/ca.pem)?$"],
  "CONDUIT_MODEL_ALLOW_INSECURE_HTTP" => ["MODEL_ALLOW_INSECURE_HTTP", "false", "^(true|false)$"]
}.each do |name, (variable_name, default, pattern)|
  abort "Hub must accept #{name} from the Zarf variable #{variable_name} and never a literal" unless hub_env[name] == "###ZARF_VAR_#{variable_name}###"
  variable = definition.fetch("variables").find { |item| item["name"] == variable_name }
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
declared_variables = definition.fetch("variables").map { |item| item["name"] }
undeclared = uds_text.scan(/###ZARF_VAR_([A-Z0-9_]+)###/).flatten.uniq - declared_variables
abort "UDS Package uses undeclared Zarf variables: #{undeclared.join(', ')}" unless undeclared.empty?
# The model egress is granted only when configured. A Zarf variable cannot
# drop a list entry, so "not configured" is a default that names no namespace
# and no workload: the rendered policy then selects nothing.
%w[MODEL_SERVICE_NAMESPACE MODEL_SERVICE_NAME_LABEL].each do |name|
  variable = definition.fetch("variables").find { |item| item["name"] == name }
  abort "#{name} must default to the sentinel that selects nothing, so no model egress is granted until an operator sets it" unless
    variable && variable["default"] == "conduit-model-unconfigured"
end
abort "only the Hub may be granted egress to the model endpoint" unless
  allows.count { |item| item.to_s.include?("ZARF_VAR_MODEL_SERVICE_") } == 1
%w[OIDC_ISSUER_NAMESPACE OIDC_ISSUER_NAME_LABEL CHART_REGISTRY_NAMESPACE CHART_REGISTRY_LABEL_KEY CHART_REGISTRY_LABEL_VALUE TEMPORAL_NAMESPACE TEMPORAL_NAME_LABEL MODEL_SERVICE_NAMESPACE MODEL_SERVICE_NAME_LABEL].each do |name|
  variable = definition.fetch("variables").find { |item| item["name"] == name }
  abort "#{name} must have a non-empty default and a pattern, so an unset variable cannot render an empty selector" unless
    variable && !variable["default"].to_s.empty? && !variable["pattern"].to_s.empty?
end
sso = uds_package.dig("spec", "sso") || []
client = sso.find { |item| item["clientId"] == "conduit-hub" }
abort "UDS Package must register the Hub SSO client" unless client
abort "UDS Package must set the console redirect URI" unless client["redirectUris"] == ["###ZARF_VAR_CONSOLE_PUBLIC_URL###/console/*"]
abort "UDS Package must not put authservice in front of the Agent mTLS port" if client["enableAuthserviceSelector"]
groups_mapper = (client["protocolMappers"] || []).find { |item| item["protocolMapper"] == "oidc-group-membership-mapper" && item.dig("config", "claim.name") == "groups" }
abort "UDS Package must map Keycloak groups into the token" unless groups_mapper
abort "UDS Package must write the issuer CA bundle for operator OIDC" unless uds_package.dig("spec", "caBundle", "configMap", "name") == "uds-trust-bundle"
# The default Hub is file-backed and serves no /metrics. A monitor registered
# against it is a target that is down for the life of the deployment, so the
# monitor lives only in the variant an operator selects together with a
# working metrics configuration.
abort "default UDS Package must not register a monitor the file-backed Hub cannot serve" if uds_package["spec"].key?("monitor")
metrics_text = File.read(File.join(root, "deploy/zarf/manifests/uds-package-metrics.yaml"))
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

abort "UDS bundle must carry the Conduit package" unless bundle["kind"] == "UDSBundle"
abort "UDS bundle version must track VERSION" unless bundle.dig("metadata", "version") == version
conduit = (bundle["packages"] || []).find { |item| item["name"] == "platform-runtime" }
abort "UDS bundle must build the local Zarf package and select the uds component" unless conduit&.dig("path") == "." && conduit["ref"] == version && conduit["optionalComponents"] == ["uds"]

example = File.join(root, "internal/packagingworkflow/fixtures/example-package")
example_definition = YAML.safe_load(File.read(File.join(example, "zarf.yaml")))
abort "generated package API must be pinned" unless example_definition["apiVersion"] == "zarf.dev/v1alpha1"
abort "generated package must install the UDS Package manifest" unless example_definition.dig("components", 0, "manifests", 0, "files") == ["uds-package.yaml"]
package_text = File.read(File.join(example, "uds-package.yaml"))
abort "generated package CR must be uds.dev/v1alpha1 Package" unless package_text.include?("apiVersion: uds.dev/v1alpha1") && package_text.include?("kind: Package")
%w[sso: allow: monitor:].each do |section|
  abort "generated package CR missing #{section}" unless package_text.include?(section)
end

zarf = ENV["ZARF_BIN"]
if ENV.key?("ZARF_BIN")
  abort "ZARF_BIN must name a Zarf executable" if zarf.to_s.empty?
  args = [zarf, "dev", "lint", File.join(root, "deploy/zarf"), "--architecture", "amd64", "--set", "PLATFORM_VERSION=0.0.0",
          "--set", "HUB_IMAGE=ghcr.io/example/platform-hub:0.0.0@sha256:#{'a' * 64}",
          "--set", "SPOKE_AGENT_IMAGE=ghcr.io/example/platform-spoke-agent:0.0.0@sha256:#{'b' * 64}"]
  abort "Zarf CLI lint failed" unless system(*args)
  abort "generated package lint failed" unless system(zarf, "dev", "lint", example, "--architecture", "amd64")
end

puts "Zarf offline-bundle package contract verified"
