#!/usr/bin/env ruby
# frozen_string_literal: true

require "json"
require "digest"
require "open3"
require "rbconfig"
require "tmpdir"
require "yaml"

root = File.expand_path("..", __dir__)
helm = ENV.fetch("HELM_BIN", "helm")
charts = {
  hub: File.join(root, "deploy/helm/platform-hub"),
  spoke: File.join(root, "deploy/helm/platform-spoke")
}
version = File.read(File.join(root, "VERSION")).strip
abort "VERSION must be semantic" unless version.match?(/\A[0-9]+\.[0-9]+\.[0-9]+\z/)

def capture(*command)
  Open3.capture3(*command)
end

def run!(*command)
  stdout, stderr, status = capture(*command)
  abort "command failed: #{command.join(' ')}\n#{stdout}\n#{stderr}" unless status.success?
  stdout
end

def render!(helm, release, chart, values, extra = [])
  YAML.load_stream(
    run!(helm, "template", release, chart, "--namespace", "platform-system", "--values", values, *extra)
  ).compact
end

def assert_fails_closed!(helm, release, chart, extra = [])
  stdout, stderr, status = capture(
    helm, "template", release, chart, "--namespace", "platform-system", *extra
  )
  abort "#{release} rendered without required production values" if status.success?
  abort "#{release} missing-value failure did not identify its cause" if (stdout + stderr).strip.empty?
end

def validate_deployment!(deployment)
  pod = deployment.dig("spec", "template", "spec")
  abort "runtime must not use a host namespace" if pod["hostNetwork"] || pod["hostPID"] || pod["hostIPC"]
  abort "runtime must use RuntimeDefault seccomp" unless pod.dig("securityContext", "seccompProfile", "type") == "RuntimeDefault"
  abort "runtime must run as non-root UID/GID 65532" unless pod.dig("securityContext", "runAsNonRoot") == true && pod.dig("securityContext", "runAsUser") == 65_532 && pod.dig("securityContext", "runAsGroup") == 65_532
  (pod["volumes"] || []).each do |volume|
    abort "runtime must not mount hostPath" if volume.key?("hostPath")
  end
  pod.fetch("containers").each do |container|
    image = container.fetch("image")
    abort "runtime image is not pinned by exact digest: #{image}" unless image.match?(/@sha256:[a-f0-9]{64}\z/)
    security = container.fetch("securityContext")
    abort "runtime permits privilege escalation" unless security["allowPrivilegeEscalation"] == false
    abort "runtime container must drop every capability" unless security.dig("capabilities", "drop") == ["ALL"]
    abort "runtime root filesystem must be read-only" unless security["readOnlyRootFilesystem"] == true
    abort "runtime contains a privileged container" if security["privileged"]
  end
end

# Every Hub this chart can render must come up as a signer its fleet already
# trusts, in every profile rather than only in the default one.
#
# Two halves, and the second is the one that was missing. A Hub carrying an
# ALLOW_INSECURE_ opt-in has declared it is not a production Hub, which no
# chart-rendered Hub may do. A Hub with no BUNDLE_SIGNING_KEY_FILE now refuses
# to start -- and before it refused, it signed Bundles with a key it generated
# for itself, so a chart profile that dropped the key would have produced a Hub
# that reported healthy while signing as a signer no target trusts.
def assert_signs_as_a_trusted_signer!(deployment, profile)
  environment = deployment.dig("spec", "template", "spec", "containers", 0, "env")
    .to_h { |item| [item["name"], item["value"]] }
  insecure = environment.keys.select { |name| name.start_with?("ALLOW_INSECURE_") }
  abort "#{profile} Hub enabled insecure opt-ins: #{insecure.join(', ')}" unless insecure.empty?
  abort "#{profile} Hub renders no BUNDLE_SIGNING_KEY_FILE and would refuse to start" if
    environment["BUNDLE_SIGNING_KEY_FILE"].to_s.strip.empty?
end

# The Spoke Agent's Kubernetes privilege, asserted rather than merely present.
#
# The Agent's token is the largest blast radius in the system and nothing in the
# repository used to say what it was bound to (#264). A rendered RBAC file that
# nobody checks is worse than none, because it reads as a bound that is not
# enforced -- so this asserts the two properties a reader would otherwise assume:
# the binding names the identity the Pod actually runs as, and no rule is a
# wildcard nobody argued for.
#
# `documents` is one rendered Spoke release; `expected_namespaces` is what
# `rbac.managedNamespaces` was set to for that render.
def assert_spoke_rbac_floor!(root, documents, expected_namespaces, profile)
  deployment = documents.find { |item| item["kind"] == "Deployment" }
  identity = deployment.dig("spec", "template", "spec", "serviceAccountName")
  release_namespace = deployment.dig("metadata", "namespace")
  roles = documents.select { |item| item["kind"] == "Role" }
  bindings = documents.select { |item| item["kind"] == "RoleBinding" }
  abort "#{profile} Spoke did not render one Role per managed namespace" unless
    roles.map { |item| item.dig("metadata", "namespace") }.sort == expected_namespaces.sort
  abort "#{profile} Spoke did not render one RoleBinding per managed namespace" unless
    bindings.map { |item| item.dig("metadata", "namespace") }.sort == expected_namespaces.sort

  bindings.each do |binding|
    namespace = binding.dig("metadata", "namespace")
    # The failure this exists for: a binding naming a ServiceAccount the
    # Deployment does not use grants nothing, and looks exactly like a grant.
    abort "#{profile} Spoke RoleBinding in #{namespace} does not name the Pod's ServiceAccount" unless
      binding.fetch("subjects") == [{"kind" => "ServiceAccount", "name" => identity, "namespace" => release_namespace}]
    reference = binding.fetch("roleRef")
    abort "#{profile} Spoke RoleBinding in #{namespace} does not reference a namespaced Role" unless
      reference["apiGroup"] == "rbac.authorization.k8s.io" && reference["kind"] == "Role"
    abort "#{profile} Spoke RoleBinding in #{namespace} references an unrendered Role" unless
      roles.any? { |role| role.dig("metadata", "namespace") == namespace && role.dig("metadata", "name") == reference["name"] }
  end

  # A wildcard is allowed only where the chart argued for that exact rule. The
  # justification lives in the template rather than here, so the argument and the
  # rule cannot be edited apart; with none written, every wildcard fails.
  justifications = File.read(File.join(root, "deploy/helm/platform-spoke/templates/rbac.yaml"))
    .scan(/# wildcard-justified: (\S+)/).flatten
  roles.each do |role|
    role.fetch("rules").each do |rule|
      abort "#{profile} Spoke Role grants non-resource URLs" if rule.key?("nonResourceURLs")
      escalating = rule.fetch("verbs") & %w[escalate bind impersonate]
      abort "#{profile} Spoke Role grants privilege-escalation verbs: #{escalating.join(', ')}" unless escalating.empty?
      %w[apiGroups resources verbs].each do |field|
        next unless rule.fetch(field, []).include?("*")

        token = "#{field}:#{rule.fetch('apiGroups', []).join(',')}/#{rule.fetch('resources', []).join(',')}"
        abort "#{profile} Spoke Role grants an unjustified wildcard #{field} (#{token})" unless justifications.include?(token)
      end
    end
  end
end

charts.each_value do |chart|
  metadata = YAML.safe_load(File.read(File.join(chart, "Chart.yaml")))
  abort "chart version drifted from VERSION" unless metadata["version"] == version && metadata["appVersion"] == version
  test_values = File.join(chart, "ci/test-values.yaml")
  run!(helm, "lint", chart, "--strict", "--values", test_values)
end

Dir.mktmpdir("platform-helm-verification-") do |directory|
  charts.each do |component, chart|
    run!(helm, "package", chart, "--destination", directory, "--version", version, "--app-version", version)
    package = File.join(directory, "platform-#{component}-#{version}.tgz")
    sbom = "#{package}.spdx.json"
    run!(RbConfig.ruby, File.join(root, "scripts/generate-helm-sbom.rb"), package, sbom, "platform-#{component}", version)
    document = JSON.parse(File.read(sbom))
    package_record = document.fetch("packages").fetch(0)
    abort "Helm SBOM does not describe its exact package" unless package_record.fetch("versionInfo") == version &&
      package_record.fetch("checksums").any? { |item| item == {"algorithm" => "SHA256", "checksumValue" => Digest::SHA256.file(package).hexdigest} }
    abort "Helm SBOM omitted chart files" if document.fetch("files").empty?
  end
end

assert_fails_closed!(helm, "platform-hub", charts.fetch(:hub))
assert_fails_closed!(helm, "platform-spoke", charts.fetch(:spoke))
assert_fails_closed!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  ["--values", File.join(charts.fetch(:hub), "ci/test-values.yaml"), "--set", "serviceAccount.create=false", "--set", "serviceAccount.name="]
)
assert_fails_closed!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  ["--values", File.join(charts.fetch(:hub), "ci/test-values.yaml"), "--set-string", "image=example.invalid/platform-hub:latest"]
)
# A Hub with no signing Secret refuses to start, so the chart refuses to render
# one. Failing here rather than at rollout is the difference between a failed
# `helm upgrade` and a CrashLoopBackOff nobody reads until a deployment stalls.
assert_fails_closed!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  ["--values", File.join(charts.fetch(:hub), "ci/test-values.yaml"), "--set-string", "bundleSigningSecretName="]
)
assert_fails_closed!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  ["--values", File.join(charts.fetch(:hub), "ci/test-values.yaml"), "--set-string", "consoleBootstrap.apiBaseURL=https://example.invalid"]
)
assert_fails_closed!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  [
    "--values", File.join(charts.fetch(:hub), "ci/test-values.yaml"),
    "--set-string", "consoleBootstrap.provenance.claimClass=tenant-observation",
    "--set-string", "consoleBootstrap.provenance.evidenceRef=conduit://console/default"
  ]
)
assert_fails_closed!(
  helm,
  "platform-spoke",
  charts.fetch(:spoke),
  ["--values", File.join(charts.fetch(:spoke), "ci/test-values.yaml"), "--set-string", "controlPlaneURL=http://platform-hub.example.test"]
)
assert_fails_closed!(
  helm,
  "platform-spoke",
  charts.fetch(:spoke),
  ["--values", File.join(charts.fetch(:spoke), "ci/test-values.yaml"), "--set", "serviceAccount.create=false", "--set", "serviceAccount.name="]
)

hub_documents = render!(helm, "platform-hub", charts.fetch(:hub), File.join(charts.fetch(:hub), "ci/test-values.yaml"))
spoke_documents = render!(helm, "platform-spoke", charts.fetch(:spoke), File.join(charts.fetch(:spoke), "ci/test-values.yaml"))
documents = hub_documents + spoke_documents
expected_kinds = %w[PodDisruptionBudget ServiceAccount ConfigMap Service Deployment ServiceAccount Role RoleBinding Deployment]
abort "unexpected chart resources: #{documents.map { |item| item['kind'] }}" unless documents.map { |item| item["kind"] } == expected_kinds
# The Spoke's Role and RoleBinding are the one thing that leaves the release
# namespace, and only into the namespaces `rbac.managedNamespaces` names. An
# Agent installs into namespaces it does not live in, so a Role confined to the
# release namespace would grant nothing where it is needed.
spoke_rbac_namespaces = %w[example-product]
documents.each do |item|
  expected = if %w[Role RoleBinding].include?(item["kind"])
    spoke_rbac_namespaces
  else
    ["platform-system"]
  end
  abort "chart escaped the release namespace: #{item['kind']}" unless expected.include?(item.dig("metadata", "namespace"))
end
# The Hub holds no Kubernetes credential at all, so any RBAC it renders is a
# regression; the Spoke may render namespaced RBAC and nothing cluster-scoped.
abort "Hub chart creates authorization it has no use for" unless
  (hub_documents.map { |item| item["kind"] } & %w[Namespace CustomResourceDefinition ClusterRole ClusterRoleBinding Role RoleBinding]).empty?
abort "Spoke chart creates cluster/bootstrap authorization" unless
  (spoke_documents.map { |item| item["kind"] } & %w[Namespace CustomResourceDefinition ClusterRole ClusterRoleBinding]).empty?
assert_spoke_rbac_floor!(root, spoke_documents, spoke_rbac_namespaces, "default")

# Fails closed rather than defaulting to the release namespace: the Agent's own
# namespace is not the namespace it installs into, so a default would be
# silently wrong in the normal topology.
assert_fails_closed!(
  helm,
  "platform-spoke",
  charts.fetch(:spoke),
  ["--values", File.join(charts.fetch(:spoke), "ci/test-values.yaml"), "--set", "rbac.managedNamespaces=null"]
)
# Both spellings of "no namespaces": absent, which the schema refuses, and
# present-but-empty, which only the template can refuse.
assert_fails_closed!(
  helm,
  "platform-spoke",
  charts.fetch(:spoke),
  ["--values", File.join(charts.fetch(:spoke), "ci/test-values.yaml"), "--set-json", '{"rbac":{"create":true,"managedNamespaces":[]}}']
)
# The escape hatch for a tenant that binds the Agent itself has to actually
# render nothing, or it is not an escape hatch.
unbound_spoke = render!(
  helm,
  "platform-spoke",
  charts.fetch(:spoke),
  File.join(charts.fetch(:spoke), "ci/test-values.yaml"),
  ["--set", "rbac.create=false"]
)
abort "rbac.create=false still rendered authorization" unless
  unbound_spoke.none? { |item| %w[Role RoleBinding ClusterRole ClusterRoleBinding].include?(item["kind"]) }

external_hub = render!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  File.join(charts.fetch(:hub), "ci/test-values.yaml"),
  ["--set", "serviceAccount.create=false", "--set", "serviceAccount.name=external-hub"]
)
external_spoke = render!(
  helm,
  "platform-spoke",
  charts.fetch(:spoke),
  File.join(charts.fetch(:spoke), "ci/test-values.yaml"),
  ["--set", "serviceAccount.create=false", "--set", "serviceAccount.name=external-spoke"]
)
abort "external Hub identity still created a ServiceAccount" if external_hub.any? { |item| item["kind"] == "ServiceAccount" }
abort "external Spoke identity still created a ServiceAccount" if external_spoke.any? { |item| item["kind"] == "ServiceAccount" }
abort "external Hub identity was not selected" unless external_hub.find { |item| item["kind"] == "Deployment" }.dig("spec", "template", "spec", "serviceAccountName") == "external-hub"
abort "external Spoke identity was not selected" unless external_spoke.find { |item| item["kind"] == "Deployment" }.dig("spec", "template", "spec", "serviceAccountName") == "external-spoke"
# An externally owned ServiceAccount is where a binding most easily ends up
# naming the wrong identity, because the name is no longer the chart's own.
assert_spoke_rbac_floor!(root, external_spoke, %w[example-product], "external-identity")
# More than one managed namespace is the production shape: one Agent, several
# application namespaces, and a separate Role in each rather than one ClusterRole.
multi_namespace_spoke = render!(
  helm,
  "platform-spoke",
  charts.fetch(:spoke),
  File.join(charts.fetch(:spoke), "ci/test-values.yaml"),
  ["--set", "rbac.managedNamespaces={example-product,example-second}"]
)
assert_spoke_rbac_floor!(root, multi_namespace_spoke, %w[example-product example-second], "multi-namespace")

hub_service_account = hub_documents.find { |item| item["kind"] == "ServiceAccount" }
abort "Hub ServiceAccount must not mount a token" unless hub_service_account&.dig("metadata", "name") == "platform-hub" && hub_service_account["automountServiceAccountToken"] == false
spoke_service_account = spoke_documents.find { |item| item["kind"] == "ServiceAccount" }
abort "Spoke ServiceAccount must use its namespaced Kubernetes identity" unless spoke_service_account&.dig("metadata", "name") == "platform-spoke" && spoke_service_account["automountServiceAccountToken"] == true

pvc = hub_documents.find { |item| item["kind"] == "PersistentVolumeClaim" }
abort "PostgreSQL Hub must not claim a local state volume" if pvc

hub = hub_documents.find { |item| item["kind"] == "Deployment" }
spoke = spoke_documents.find { |item| item["kind"] == "Deployment" }
validate_deployment!(hub)
validate_deployment!(spoke)
abort "production Hub unexpectedly grants supplemental groups" if hub.dig("spec", "template", "spec", "securityContext").key?("supplementalGroups")
abort "production Spoke unexpectedly grants supplemental groups" if spoke.dig("spec", "template", "spec", "securityContext").key?("supplementalGroups")
abort "Hub reserved Pod identity was overridden" unless hub.dig("spec", "template", "metadata", "labels", "app.kubernetes.io/component") == "hub"
abort "Spoke reserved Pod identity was overridden" unless spoke.dig("spec", "template", "metadata", "labels", "app.kubernetes.io/component") == "spoke"
abort "Hub image pull identity was omitted" unless hub.dig("spec", "template", "spec", "imagePullSecrets") == [{"name" => "platform-registry"}]
abort "Spoke image pull identity was omitted" unless spoke.dig("spec", "template", "spec", "imagePullSecrets") == [{"name" => "platform-registry"}]

k3d_hub = render!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  File.join(charts.fetch(:hub), "ci/test-values.yaml"),
  ["--set", "runtimeSupplementalGroups[0]=0"]
).find { |item| item["kind"] == "Deployment" }
k3d_spoke = render!(
  helm,
  "platform-spoke",
  charts.fetch(:spoke),
  File.join(charts.fetch(:spoke), "ci/test-values.yaml"),
  ["--set", "runtimeSupplementalGroups[0]=0", "--set", "helmPlainHTTP=true"]
).find { |item| item["kind"] == "Deployment" }
abort "Hub runtime compatibility group did not render exactly" unless k3d_hub.dig("spec", "template", "spec", "securityContext", "supplementalGroups") == [0]
abort "Spoke runtime compatibility group did not render exactly" unless k3d_spoke.dig("spec", "template", "spec", "securityContext", "supplementalGroups") == [0]
local_spoke_env = k3d_spoke.dig("spec", "template", "spec", "containers", 0, "env").to_h { |item| [item["name"], item["value"]] }
abort "Spoke local-registry compatibility did not render exactly" unless local_spoke_env["HELM_PLAIN_HTTP"] == "true"

hub_container = hub.dig("spec", "template", "spec", "containers", 0)
abort "transactional Hub must render two replicas" unless hub.dig("spec", "replicas") == 2
abort "transactional Hub must use a zero-unavailable rolling update" unless
  hub.dig("spec", "strategy", "type") == "RollingUpdate" &&
    hub.dig("spec", "strategy", "rollingUpdate") == {"maxUnavailable" => 0, "maxSurge" => 1}
pdb = hub_documents.find { |item| item["kind"] == "PodDisruptionBudget" }
abort "transactional Hub must protect one replica from voluntary disruption" unless
  pdb&.dig("spec", "maxUnavailable") == 1 &&
    pdb.dig("spec", "selector", "matchLabels", "app.kubernetes.io/component") == "hub"
abort "Hub probes must use HTTPS" unless %w[startupProbe readinessProbe livenessProbe].all? { |probe| hub_container.dig(probe, "httpGet", "scheme") == "HTTPS" }
hub_env = hub_container.fetch("env").to_h { |item| [item["name"], item["value"]] }
required_hub_env = %w[REPOSITORY_KIND POSTGRES_DSN_FILE AGENT_CLIENT_CA_FILE TLS_CERT_FILE TLS_KEY_FILE BUNDLE_SIGNING_KEY_FILE CONSOLE_BOOTSTRAP_FILE PLATFORM_BOOTSTRAP_ADMINISTRATORS]
abort "Hub production identity/signing inputs are incomplete" unless (required_hub_env - hub_env.keys).empty?
abort "chart enabled insecure identity" if hub_env.keys.any? { |name| name.start_with?("ALLOW_INSECURE_") }
abort "Hub console bootstrap path drifted" unless hub_env["CONSOLE_BOOTSTRAP_FILE"] == "/etc/conduit/console/bootstrap.json"
abort "Hub PostgreSQL mode drifted" unless hub_env["REPOSITORY_KIND"] == "postgres" && hub_env["POSTGRES_DSN_FILE"] == "/run/conduit-database/dsn"
database_mount = hub_container.fetch("volumeMounts").find { |item| item["name"] == "database" }
database_volume = hub.dig("spec", "template", "spec", "volumes").find { |item| item["name"] == "database" }
abort "PostgreSQL DSN must come from one read-only Secret mount" unless
  database_mount == {"name" => "database", "mountPath" => "/run/conduit-database", "readOnly" => true} &&
    database_volume&.dig("secret", "secretName") == "platform-postgres" &&
    database_volume.dig("secret", "items") == [{"key" => "dsn", "path" => "dsn"}]

file_hub_documents = render!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  File.join(charts.fetch(:hub), "ci/test-values.yaml"),
  ["--set", "repository.kind=file", "--set", "replicaCount=1"]
)
file_hub = file_hub_documents.find { |item| item["kind"] == "Deployment" }
file_pvc = file_hub_documents.find { |item| item["kind"] == "PersistentVolumeClaim" }
file_env = file_hub.dig("spec", "template", "spec", "containers", 0, "env").to_h { |item| [item["name"], item["value"]] }
abort "file repository must stay one replica with Recreate" unless
  file_hub.dig("spec", "replicas") == 1 && file_hub.dig("spec", "strategy", "type") == "Recreate"
abort "file repository must retain its durable PVC" unless
  file_pvc&.dig("metadata", "annotations", "helm.sh/resource-policy") == "keep"
abort "file repository path drifted" unless
  file_env["REPOSITORY_KIND"] == "file" && file_env["DATA_DIRECTORY"] == "/var/lib/platform-delivery"
abort "file repository must not render a disruption budget" if file_hub_documents.any? { |item| item["kind"] == "PodDisruptionBudget" }

# The signing posture, over every Hub profile this chart renders rather than
# only the PostgreSQL default. File mode is the development/migration profile
# and is exactly where an ephemeral signer would look reasonable and be wrong:
# it is still a Hub whose Bundles reach targets.
{
  "PostgreSQL" => hub, "file-repository" => file_hub, "external-identity" => external_hub.find { |item| item["kind"] == "Deployment" },
  "k3d-compatibility" => k3d_hub
}.each { |profile, deployment| assert_signs_as_a_trusted_signer!(deployment, profile) }

bootstrap_config = hub_documents.find { |item| item["kind"] == "ConfigMap" }
abort "Hub console bootstrap ConfigMap is missing" unless bootstrap_config&.dig("metadata", "name") == "platform-hub-console-bootstrap"
bootstrap_raw = bootstrap_config.dig("data", "bootstrap.json")
bootstrap = JSON.parse(bootstrap_raw)
expected_bootstrap = {
  "schemaVersion" => "conduit.console.bootstrap/v1",
  "tenant" => {"id" => "conduit", "displayName" => "Conduit"},
  "brand" => {"title" => "Conduit", "subtitle" => "Control plane", "accent" => "#2d72d2", "icon" => "dashboard"},
  "provenance" => {"claimClass" => "conduit-extension", "evidenceRef" => "conduit://console/default"}
}
abort "Hub console bootstrap default drifted" unless bootstrap == expected_bootstrap
bootstrap_checksum = hub.dig("spec", "template", "metadata", "annotations", "checksum/console-bootstrap")
abort "Hub console bootstrap checksum is not exact" unless bootstrap_checksum == Digest::SHA256.hexdigest(bootstrap_raw)
bootstrap_mount = hub_container.fetch("volumeMounts").find { |item| item["name"] == "console-bootstrap" }
abort "Hub console bootstrap is not mounted read-only" unless bootstrap_mount == {
  "name" => "console-bootstrap", "mountPath" => "/etc/conduit/console", "readOnly" => true
}
bootstrap_volume = hub.dig("spec", "template", "spec", "volumes").find { |item| item["name"] == "console-bootstrap" }
abort "Hub console bootstrap does not use its bounded ConfigMap" unless bootstrap_volume&.dig("configMap", "name") == "platform-hub-console-bootstrap"

scanner_hub = render!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  File.join(charts.fetch(:hub), "ci/test-values.yaml"),
  [
    "--set-string", "artifactScanner.command=/platform-grype-scanner",
    "--set-string", "artifactScanner.grypeBinaryPath=/opt/platform-scanner/grype",
    "--set-string", "artifactScanner.grypeConfigFile=/opt/platform-scanner/grype-empty.yaml",
    "--set-string", "artifactScanner.grypeCacheDirectory=/var/lib/platform-scanner/db",
    "--set-string", "artifactScanner.toolsPersistentVolumeClaimName=scanner-tools",
    "--set-string", "artifactScanner.cachePersistentVolumeClaimName=scanner-cache",
    "--set-string", "artifactScanner.registryAuthSecretName=scanner-registry"
  ]
).find { |item| item["kind"] == "Deployment" }
scanner_pod = scanner_hub.dig("spec", "template", "spec")
scanner_container = scanner_pod.fetch("containers").first
scanner_env = scanner_container.fetch("env").to_h { |item| [item["name"], item["value"]] }
expected_scanner_env = {
  "EXTERNAL_ARTIFACT_SCANNER" => "/platform-grype-scanner",
  "EXTERNAL_ARTIFACT_SCANNER_TIMEOUT" => "2m",
  "GRYPE_BIN" => "/opt/platform-scanner/grype",
  "GRYPE_CONFIG_FILE" => "/opt/platform-scanner/grype-empty.yaml",
  "GRYPE_DB_CACHE_DIR" => "/var/lib/platform-scanner/db",
  "DOCKER_CONFIG" => "/run/platform-scanner-registry"
}
abort "Hub scanner environment did not render exactly" unless expected_scanner_env.all? { |name, value| scanner_env[name] == value }
scanner_volumes = scanner_pod.fetch("volumes").to_h { |item| [item["name"], item] }
abort "scanner tool PVC was not externally supplied" unless scanner_volumes.dig("artifact-scanner-tools", "persistentVolumeClaim", "claimName") == "scanner-tools"
abort "scanner cache PVC was not externally supplied" unless scanner_volumes.dig("artifact-scanner-cache", "persistentVolumeClaim", "claimName") == "scanner-cache"
abort "scanner registry identity was not externally supplied" unless scanner_volumes.dig("artifact-scanner-registry-auth", "secret", "secretName") == "scanner-registry"
abort "scanner chart introduced another container" unless scanner_pod.fetch("containers").length == 1
assert_signs_as_a_trusted_signer!(scanner_hub, "artifact-scanner")

spoke_env = spoke.dig("spec", "template", "spec", "containers", 0, "env").to_h { |item| [item["name"], item["value"]] }
required_spoke_env = %w[CONTROL_PLANE_URL CONTROL_PLANE_CA_FILE CONTROL_PLANE_CLIENT_CERT_FILE CONTROL_PLANE_CLIENT_KEY_FILE ENVIRONMENT_ID AGENT_ID]
abort "Spoke mTLS inputs are incomplete" unless (required_spoke_env - spoke_env.keys).empty?
abort "Spoke control plane transport must use HTTPS" unless spoke_env["CONTROL_PLANE_URL"]&.start_with?("https://")
abort "production Spoke enabled plaintext Helm registry access" if spoke_env.key?("HELM_PLAIN_HTTP")

rubix_hub = render!(
  helm,
  "platform-hub",
  charts.fetch(:hub),
  File.join(charts.fetch(:hub), "rubix-values.example.yaml"),
  [
    "--set-string", 'service.annotations.com\\.palantir\\.rubix\\.service/pod-cert=conflicting-user-value',
    "--set-string", 'service.annotations.com\\.palantir\\.rubix\\.service\\.spp/v3=conflicting-user-value'
  ]
)
rubix_spoke = render!(helm, "platform-spoke", charts.fetch(:spoke), File.join(charts.fetch(:spoke), "rubix-values.example.yaml"))
rubix_service = rubix_hub.find { |item| item["kind"] == "Service" }
annotations = rubix_service&.dig("metadata", "annotations") || {}
abort "Rubix Hub Service must request a Pod certificate" unless annotations["com.palantir.rubix.service/pod-cert"] == "{}"
spp = JSON.parse(annotations.fetch("com.palantir.rubix.service.spp/v3"))
endpoint = spp.fetch("endpoints").first
abort "Rubix SPP endpoint is incomplete" unless endpoint == {"domain-aliases" => ["replace-me.example.internal"], "name" => "service", "prefix" => "/platform"}
assert_signs_as_a_trusted_signer!(rubix_hub.find { |item| item["kind"] == "Deployment" }, "Rubix")

(rubix_hub + rubix_spoke).select { |item| item["kind"] == "Deployment" }.each do |deployment|
  terms = deployment.dig("spec", "template", "spec", "affinity", "nodeAffinity", "requiredDuringSchedulingIgnoredDuringExecution", "nodeSelectorTerms") || []
  expressions = terms.flat_map { |term| term["matchExpressions"] || [] }
  abort "Rubix runtime lacks required instance-group affinity" unless expressions.any? { |expression| expression["key"] == "com.palantir.rubix/instance-group" && expression["operator"] == "In" }
end
abort "Rubix profiles must mount the external CA bundle" unless (rubix_hub + rubix_spoke).to_s.include?("tls-external-ca-pem-bundle")

puts "Two-chart Helm runtime and bounded Rubix profile verified with #{run!(helm, 'version', '--short').strip}"
