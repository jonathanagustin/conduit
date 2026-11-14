#!/usr/bin/env ruby
# frozen_string_literal: true

require "yaml"

root = File.expand_path("..", __dir__)
definition_path = File.join(root, "deploy/zarf/zarf.yaml")
manifest_path = File.join(root, "deploy/zarf/manifests/runtime.yaml")
definition_text = File.read(definition_path)
manifest_text = File.read(manifest_path)
definition = YAML.safe_load(definition_text)
manifests = YAML.load_stream(manifest_text)

abort "Zarf package API must be pinned" unless definition["apiVersion"] == "zarf.dev/v1alpha1"
abort "Zarf package version must come from the release" unless definition.dig("metadata", "version") == "###ZARF_PKG_TMPL_PLATFORM_VERSION###"
abort "Zarf package components drifted" unless definition.fetch("components").map { |item| [item["name"], item["required"]] } == [["runtime", true], ["packaging-worker", false]]
worker = definition.fetch("components").find { |item| item["name"] == "packaging-worker" }
abort "optional packaging worker must reuse the Hub image" unless worker&.dig("images") == ["###ZARF_PKG_TMPL_HUB_IMAGE###"]
abort "optional packaging worker must not be required" if worker["required"]

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
if zarf && !zarf.empty?
  args = [zarf, "dev", "lint", File.join(root, "deploy/zarf"), "--architecture", "amd64", "--set", "PLATFORM_VERSION=0.0.0",
          "--set", "HUB_IMAGE=ghcr.io/example/platform-hub:0.0.0@sha256:#{'a' * 64}",
          "--set", "SPOKE_AGENT_IMAGE=ghcr.io/example/platform-spoke-agent:0.0.0@sha256:#{'b' * 64}"]
  abort "Zarf CLI lint failed" unless system(*args)
  abort "generated package lint failed" unless system(zarf, "dev", "lint", example, "--architecture", "amd64")
end

puts "Zarf air-gap package contract verified"
