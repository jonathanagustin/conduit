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
abort "Zarf package components drifted" unless definition.fetch("components").map { |item| [item["name"], item["required"], item["default"]] } == [["runtime", true, nil], ["packaging-worker", false, nil], ["uds", false, false]]
worker = definition.fetch("components").find { |item| item["name"] == "packaging-worker" }
abort "optional packaging worker must reuse the Hub image" unless worker&.dig("images") == ["###ZARF_PKG_TMPL_HUB_IMAGE###"]
abort "optional packaging worker must not be required" if worker["required"]
uds_component = definition.fetch("components").find { |item| item["name"] == "uds" }
abort "UDS component must stay optional and unselected by a plain install" if uds_component["required"] || uds_component["default"]
abort "UDS component must not carry images" if uds_component["images"]
abort "UDS component must apply only the Package CR" unless uds_component.dig("manifests", 0, "files") == ["manifests/uds-package.yaml"]
runtime_files = definition.dig("components", 0, "manifests", 0, "files")
abort "runtime component must not apply the UDS Package CR" if runtime_files.include?("manifests/uds-package.yaml")

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
sso = uds_package.dig("spec", "sso") || []
client = sso.find { |item| item["clientId"] == "conduit-hub" }
abort "UDS Package must register the Hub SSO client" unless client
abort "UDS Package must set the console redirect URI" unless client["redirectUris"] == ["###ZARF_VAR_CONSOLE_PUBLIC_URL###/console/*"]
abort "UDS Package must not put authservice in front of the Agent mTLS port" if client["enableAuthserviceSelector"]
groups_mapper = (client["protocolMappers"] || []).find { |item| item["protocolMapper"] == "oidc-group-membership-mapper" && item.dig("config", "claim.name") == "groups" }
abort "UDS Package must map Keycloak groups into the token" unless groups_mapper
abort "UDS Package must write the issuer CA bundle for operator OIDC" unless uds_package.dig("spec", "caBundle", "configMap", "name") == "uds-trust-bundle"
monitor = uds_package.dig("spec", "monitor") || []
abort "UDS Package must declare a ServiceMonitor for the metrics port" unless monitor.any? { |item| item["kind"] == "ServiceMonitor" && item["portName"] == "metrics" && item["targetPort"] == 9090 && item["path"] == "/metrics" }

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
