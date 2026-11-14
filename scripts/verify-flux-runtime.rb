#!/usr/bin/env ruby
# frozen_string_literal: true

require "yaml"

root = File.expand_path("..", __dir__)
runtime = File.join(root, "deploy/flux/runtime/delivery-plane.yaml")
rbac = File.join(root, "deploy/flux/runtime-rbac.yaml")
source = File.join(root, "deploy/flux/source.example.yaml")
spire_overlay = File.join(root, "deploy/flux/overlays/spire/runtime-spiffe-patch.yaml")

runtime_text = File.read(runtime)
abort "Hub internal Git polling must not be configured" if runtime_text.include?("GITOPS_")

required = %w[
  PLATFORM_HUB_IMAGE
  PLATFORM_SPOKE_AGENT_IMAGE
  PLATFORM_ENVIRONMENT_ID
  PLATFORM_AGENT_ID
  PLATFORM_ALLOW_INSECURE_AGENT_IDENTITY_HEADERS
  PLATFORM_ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS
  PLATFORM_ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY
  PLATFORM_BOOTSTRAP_ADMINISTRATORS
  PLATFORM_HELM_PLAIN_HTTP
]
required.each do |variable|
  abort "missing Flux substitution #{variable}" unless runtime_text.include?("${#{variable}}")
end
abort "string and boolean substitutions must use Flux's quote variable" unless runtime_text.scan("${quote}").length == 10
# Every insecure opt-in in this base must be a substitution the deployment
# supplies, never a value the runtime hard-codes. A base that shipped "true"
# would make an insecure development posture the default for anybody who
# applied it without reading it.
runtime_text.scan(/ALLOW_INSECURE_[A-Z_]+/).uniq.each do |name|
  abort "Flux runtime hard-codes an insecure opt-in: #{name}" if
    runtime_text.match?(/name: #{name}\s*\n\s*value: ["']?true/)
end
example_text = File.read(source)
abort "the Flux example must leave the ephemeral Bundle signing opt-in off" unless
  example_text.include?('PLATFORM_ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY: "false"')
abort "the SPIRE production overlay must mount a Bundle signing key" unless
  File.read(spire_overlay).include?("BUNDLE_SIGNING_KEY_FILE")

runtime_kinds = YAML.load_stream(runtime_text).map { |document| document.fetch("kind") }
expected_runtime_kinds = %w[PersistentVolumeClaim Deployment Service Deployment]
abort "unexpected Flux runtime resources" unless runtime_kinds == expected_runtime_kinds

runtime_deployments = YAML.load_stream(runtime_text).select { |document| document["kind"] == "Deployment" }
runtime_deployments.each do |deployment|
  pod_spec = deployment.dig("spec", "template", "spec")
  abort "production runtime must not require privileged initialization" unless (pod_spec["initContainers"] || []).empty?
  (pod_spec["containers"] || []).each do |container|
    security = container.fetch("securityContext")
    abort "runtime containers must drop every capability" unless security.dig("capabilities", "drop") == ["ALL"]
    abort "runtime containers must forbid privilege escalation" unless security["allowPrivilegeEscalation"] == false
  end
end

rbac_documents = YAML.load_stream(File.read(rbac))
role = rbac_documents.find { |document| document["kind"] == "Role" }
replica_set_rule = role.fetch("rules").find do |rule|
  rule.fetch("apiGroups").include?("apps") && rule.fetch("resources").include?("replicasets")
end
abort "Flux health checks require read-only ReplicaSet access" unless replica_set_rule&.fetch("verbs")&.sort == %w[get list watch]

source_documents = YAML.load_stream(File.read(source))
source_repository = source_documents.find { |document| document["kind"] == "OCIRepository" }
kustomization = source_documents.find { |document| document["kind"] == "Kustomization" }
abort "runtime source must be an OCIRepository" unless source_repository
abort "runtime example must use the repository-linked Conduit OCI package" unless
  source_repository.dig("spec", "url") == "oci://ghcr.io/example/conduit"
runtime_ref = source_repository.dig("spec", "ref") || {}
abort "runtime source must be pinned by exact digest" unless
  runtime_ref["digest"].to_s.match?(/\Asha256:[a-f0-9]{64}\z/)
abort "runtime source must not use a mutable tag or semver range" if
  runtime_ref.key?("tag") || runtime_ref.key?("semver")
abort "private runtime source must name a read-only pull Secret" unless
  source_repository.dig("spec", "secretRef", "name") == "conduit-runtime-pull"
runtime_verify = source_repository.dig("spec", "verify") || {}
runtime_identities = runtime_verify["matchOIDCIdentity"] || []
abort "runtime source must require Cosign verification" unless runtime_verify["provider"] == "cosign"
abort "runtime source must trust only the Conduit release workflow on semantic tags" unless
  runtime_identities == [{
    "issuer" => '^https://token\.actions\.githubusercontent\.com$',
    "subject" => '^https://github\.com/jonathanagustin/conduit/\.github/workflows/platform-images\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$'
  }]
substitutions = kustomization.dig("spec", "postBuild", "substitute") || {}
abort "example must define every runtime substitution" unless (required - substitutions.keys).empty?
abort "example must define a literal quote substitution" unless substitutions["quote"] == '"'
abort "Flux runtime must prune" unless kustomization.dig("spec", "prune") == true
abort "Flux runtime must wait for health" unless kustomization.dig("spec", "wait") == true
abort "Flux runtime must impersonate its bounded ServiceAccount" unless kustomization.dig("spec", "serviceAccountName") == "platform-runtime-reconciler"
abort "base runtime path must be explicit" unless kustomization.dig("spec", "path") == "./runtime"

spire_documents = YAML.load_stream(File.read(spire_overlay))
hub_patch, agent_patch = spire_documents
abort "SPIRE overlay must patch both runtime Deployments" unless [hub_patch, agent_patch].all? { |item| item["kind"] == "Deployment" }
hub_spec = hub_patch.dig("spec", "template", "spec")
agent_spec = agent_patch.dig("spec", "template", "spec")
abort "SPIRE Hub must use a dedicated ServiceAccount" unless hub_spec["serviceAccountName"] == "platform-delivery"
hub_container = hub_spec.fetch("containers").find { |item| item["name"] == "delivery-control-plane" }
agent_container = agent_spec.fetch("containers").find { |item| item["name"] == "spoke-agent" }
hub_env = hub_container.fetch("env").to_h { |item| [item["name"], item["value"]] }
agent_env = agent_container.fetch("env").to_h { |item| [item["name"], item["value"]] }
expected_spire_socket = "unix:///run/spire/sockets/socket"
abort "SPIRE Hub Workload API socket is missing" unless hub_env["SPIFFE_ENDPOINT_SOCKET"] == expected_spire_socket
abort "SPIRE Agent Workload API socket is missing" unless agent_env["SPIFFE_ENDPOINT_SOCKET"] == hub_env["SPIFFE_ENDPOINT_SOCKET"]
abort "Agent must pin the exact Hub SPIFFE ID" unless agent_env["CONTROL_PLANE_SPIFFE_ID"] == "spiffe://platform.delivery/hub"
abort "SPIFFE control plane URL must use HTTPS" unless agent_env["CONTROL_PLANE_URL"]&.start_with?("https://")
abort "SPIFFE production Hub must use a persistent signing key" unless hub_env["BUNDLE_SIGNING_KEY_FILE"] == "/run/platform-signing/bundle-signing-key.pem"
abort "SPIFFE production probes must use HTTPS" unless %w[startupProbe readinessProbe livenessProbe].all? do |probe|
  hub_container.dig(probe, "httpGet", "scheme") == "HTTPS"
end

# The tenant chart holds authority the runtime reconciler is forbidden to have,
# because it creates its own Namespace and a cluster-scoped ClusterSecretStore.
# That makes its identity and its pinning worth gating rather than trusting.
tenant_source = File.join(root, "deploy/flux/foundry-tenant.example.yaml")
tenant_rbac = File.join(root, "deploy/flux/foundry-tenant-rbac.yaml")

tenant_documents = YAML.load_stream(File.read(tenant_source))
tenant_repository = tenant_documents.find { |document| document["kind"] == "OCIRepository" }
tenant_release = tenant_documents.find { |document| document["kind"] == "HelmRelease" }
abort "tenant example must define an OCIRepository and a HelmRelease" unless tenant_repository && tenant_release
tenant_ref = tenant_repository.dig("spec", "ref") || {}
abort "tenant chart must be pinned by exact digest" unless tenant_ref["digest"].to_s.match?(/\Asha256:[a-f0-9]{64}\z/)
abort "tenant chart must not be selected by a mutable tag or semver range" if tenant_ref.key?("tag") || tenant_ref.key?("semver")
abort "private tenant chart must name a pull Secret" if tenant_repository.dig("spec", "secretRef", "name").to_s.empty?
tenant_verify = tenant_repository.dig("spec", "verify") || {}
tenant_identities = tenant_verify["matchOIDCIdentity"] || []
abort "tenant chart must require Cosign verification" unless tenant_verify["provider"] == "cosign"
abort "tenant chart must name exactly one trusted publisher identity" unless
  tenant_identities.length == 1 && tenant_identities.first["issuer"] == '^https://token\.actions\.githubusercontent\.com$' &&
    tenant_identities.first["subject"].to_s.match?(/\A\^https:\/\/github\\\.com\/.+@refs\/tags\/v/)
abort "tenant release must impersonate its bounded ServiceAccount" unless
  tenant_release.dig("spec", "serviceAccountName") == "foundry-tenant-reconciler"
abort "tenant release must resolve its chart by reference, not a mutable chart spec" unless
  tenant_release.dig("spec", "chartRef", "kind") == "OCIRepository"
tenant_values = tenant_release.dig("spec", "values") || {}
abort "tenant release must state the role its reads assume" unless
  tenant_values.dig("secretStore", "roleArn").to_s != ""
# Every rendered resource is an external-secrets kind, so a tenant reconciled
# before the CRDs exist fails on unknown types rather than waiting for them.
tenant_dependencies = tenant_release.dig("spec", "dependsOn") || []
abort "tenant release must order itself after external-secrets" unless
  tenant_dependencies.any? { |dependency| dependency["name"].to_s.include?("external-secrets") }
# An ExternalSecret only reports Ready once it has fetched from AWS. Waiting
# would tie the release to state outside the cluster, so one absent Secrets
# Manager key fails the install and remediation removes the whole tenant.
%w[install upgrade].each do |action|
  abort "tenant #{action} must not wait on external secret material" unless
    tenant_release.dig("spec", action, "disableWait") == true
end

tenant_rbac_documents = YAML.load_stream(File.read(tenant_rbac))
tenant_cluster_role = tenant_rbac_documents.find { |document| document["kind"] == "ClusterRole" }
abort "tenant reconciler must declare a ClusterRole" unless tenant_cluster_role
tenant_rules = tenant_cluster_role.fetch("rules")
abort "tenant reconciler must not hold wildcard authority" if tenant_rules.any? do |rule|
  (rule["apiGroups"].to_a + rule["resources"].to_a + rule["verbs"].to_a).include?("*")
end
namespace_rule = tenant_rules.find do |rule|
  rule.fetch("apiGroups").include?("") && rule.fetch("resources").include?("namespaces")
end
abort "tenant reconciler must be able to create its Namespace" unless namespace_rule
abort "pruning a tenant must not delete its Namespace" if namespace_rule.fetch("verbs").include?("delete")
tenant_binding = tenant_rbac_documents.find { |document| document["kind"] == "ClusterRoleBinding" }
abort "tenant reconciler must not bind a shared or built-in role" unless
  tenant_binding&.dig("roleRef", "name") == "foundry-tenant-reconciler"

# Helm keeps release state as Secrets in the storage namespace; without this the
# first reconcile fails before it ever reads the chart. It must stay namespaced.
storage_role = tenant_rbac_documents.find do |document|
  document["kind"] == "Role" && document.dig("metadata", "name").to_s.include?("release-storage")
end
abort "tenant reconciler needs Helm release storage access" unless storage_role
abort "Helm release storage access must stay namespaced to flux-system" unless
  storage_role.dig("metadata", "namespace") == "flux-system"
abort "tenant reconciler must not read Secrets cluster-wide" if tenant_rules.any? do |rule|
  rule.fetch("apiGroups").include?("") && rule.fetch("resources").include?("secrets")
end

# The static source contract above is necessary but cannot prove that the
# selected tools interoperate. Keep the expensive clean-cluster proof manual,
# while making its adversarial coverage and manual-only workflow part of the
# ordinary repository gate.
acceptance = File.read(File.join(root, "scripts/test-gitless-clean-cluster.sh"))
acceptance_requirements = {
  "checksum-pinned k3d" => "1ac1da365236736a8df8c32107b54aca208384ab1d9a06771443c85ad698a5eb",
  "checksum-pinned Flux" => "c2c397a52930f52d2005c01d276116b059d062de379386d58e98115380a766a2",
  "reviewed source-built Cosign" => '"${repo_root}/scripts/build-cosign.sh" "${bin_dir}/cosign"',
  "checksum-pinned Zot" => "a32e42d042d1f17b5b1317e55cc1a415a744c873dcd05c25c56b665478258bcb",
  "checksum-pinned ORAS" => "9ce999f8d2de03fc03968b29d743077a58783e545e5eaa53917ca177352d0e59",
  "clean ephemeral cluster" => "k3d cluster create",
  "Git-free Flux install" => "--components=source-controller,kustomize-controller",
  "pull-only authority check" => "cluster pull identity unexpectedly has registry write authority",
  "exact digest source" => "digest: ${right_digest}",
  "Cosign verification" => "provider: cosign",
  "wrong-signer rejection" => ".reason == \"VerificationError\"",
  "exact applied revision" => ".status.lastAppliedRevision == $digest",
  "zero Git source inventory" => "gitrepositories.source.toolkit.fluxcd.io",
  "zero Git credential inventory" => "kubernetes.io/ssh-auth"
}
acceptance_requirements.each do |description, fragment|
  abort "clean-cluster acceptance is missing #{description}" unless acceptance.include?(fragment)
end

acceptance_workflow = File.read(File.join(root, ".github/workflows/platform-gitless-e2e.yml"))
abort "Gitless acceptance workflow must remain manually dispatched" unless
  acceptance_workflow.include?("on:\n  workflow_dispatch:")
abort "Gitless acceptance workflow must not gain an automatic trigger" if
  acceptance_workflow.match?(/^\s{2}(push|pull_request|schedule):/)
abort "Gitless acceptance workflow must run the clean-cluster proof" unless
  acceptance_workflow.include?("./scripts/test-gitless-clean-cluster.sh")

puts "Flux runtime source and bounded reconciliation contract verified"
puts "Foundry tenant GitOps source, pinning, and bounded tenant identity verified"
puts "Gitless clean-cluster acceptance contract and manual workflow verified"
