#!/usr/bin/env ruby
# frozen_string_literal: true

# Real, pinned Zarf package selection with action-only fixture components.
# The component names/defaults/groups, variables, and deploy guards are copied
# from the shipped definition. No image, chart, manifest, or cluster is used.
require "fileutils"
require "open3"
require "tmpdir"
require "yaml"
require "json"
require_relative "lib/zarf_runtime_values"

root = File.expand_path("..", __dir__)
directory = File.join(root, "deploy/zarf")
definition = ConduitZarfRuntimeValues.definition(directory, {"HUB_IMAGE" => "fixture-hub", "SPOKE_AGENT_IMAGE" => "fixture-spoke"})
zarf = ENV.fetch("ZARF_BIN", "zarf")
version, error, status = Open3.capture3(zarf, "version")
abort "requires pinned Zarf v1.0.1: #{version}#{error}" unless status.success? && version.strip == "v1.0.1"

components = definition.fetch("components").to_h { |item| [item.fetch("name"), item] }
abort "runtime must remain required by default" if components.fetch("runtime")["optional"]
abort "exactly one optional UDS owner must exist" unless components.keys.grep(/\Auds(?:-|\z)/).sort == %w[uds uds-observability]
abort "UDS must remain optional" unless components.fetch("uds")["optional"] == true

# Defaults agree with the existing typed Helm settings. In particular, no
# Secret, ConfigMap, PVC, or executable is silently chosen for an operator.
hub = YAML.safe_load_file(File.join(root, "deploy/helm/platform-hub/values.yaml"))
spoke = YAML.safe_load_file(File.join(root, "deploy/helm/platform-spoke/values.yaml"))
defaults = YAML.safe_load_file(File.join(directory, "values.yaml"))
operational_defaults = {
  "NOTIFICATION_TRANSPORT" => hub.dig("notifications", "transport"),
  "NOTIFICATION_WEBHOOK_SECRET_NAME" => hub.dig("notifications", "webhook", "urlSecretName"),
  "NOTIFICATION_WEBHOOK_SECRET_KEY" => hub.dig("notifications", "webhook", "urlSecretKey"),
  "NOTIFICATION_WEBHOOK_TIMEOUT" => hub.dig("notifications", "webhook", "timeout"),
  "NOTIFICATION_NATIVE_TIMEOUT" => hub.dig("notifications", "native", "timeout"),
  "NOTIFICATION_SMTP_ADDRESS" => hub.dig("notifications", "native", "smtp", "address"),
  "NOTIFICATION_SMTP_FROM" => hub.dig("notifications", "native", "smtp", "from"),
  "NOTIFICATION_SMTP_TLS_MODE" => hub.dig("notifications", "native", "smtp", "tlsMode"),
  "NOTIFICATION_SMTP_CREDENTIALS_SECRET_NAME" => hub.dig("notifications", "native", "smtp", "credentialsSecretName"),
  "NOTIFICATION_SMTP_USERNAME_KEY" => hub.dig("notifications", "native", "smtp", "usernameKey"),
  "NOTIFICATION_SMTP_PASSWORD_KEY" => hub.dig("notifications", "native", "smtp", "passwordKey"),
  "NOTIFICATION_SMTP_CA_CONFIGMAP_NAME" => hub.dig("notifications", "native", "smtp", "caConfigMapName"),
  "NOTIFICATION_SMTP_CA_KEY" => hub.dig("notifications", "native", "smtp", "caKey"),
  "NOTIFICATION_SLACK_TOKEN_SECRET_NAME" => hub.dig("notifications", "native", "slack", "tokenSecretName"),
  "NOTIFICATION_SLACK_TOKEN_KEY" => hub.dig("notifications", "native", "slack", "tokenKey"),
  "PLAN_ORCHESTRATOR" => hub.dig("planOrchestrator", "driver"),
  "PLAN_TEMPORAL_ADDRESS" => hub.dig("planOrchestrator", "temporalAddress"),
  "MATERIAL_PREPARER_COMMAND" => spoke.dig("materialPreparer", "command"),
  "MATERIAL_PREPARER_TOOLS_PVC" => spoke.dig("materialPreparer", "toolsPersistentVolumeClaimName"),
  "MATERIAL_PREPARER_TOOLS_MOUNT_PATH" => spoke.dig("materialPreparer", "toolsMountPath")
}
operational_defaults.each { |name, expected| abort "#{name} default differs from Helm" unless defaults.fetch(name) == expected }

cases = 0
Dir.mktmpdir("conduit-zarf-operational-") do |work|
  marker = File.join(work, "selected")
  # These are selection/guard witnesses, not substitutes for runtime manifests.
  fixture_components = definition.fetch("components").map do |item|
    selected = item.select { |key, _| %w[name optional actions].include?(key) }
    before = Marshal.load(Marshal.dump(item.dig("actions", "onDeploy", "before") || []))
    if item.fetch("name") == "runtime"
      %w[HUB_OPERATIONAL_ENV HUB_OPERATIONAL_MOUNTS HUB_OPERATIONAL_VOLUMES SPOKE_OPERATIONAL_ENV SPOKE_OPERATIONAL_MOUNTS SPOKE_OPERATIONAL_VOLUMES].each do |name|
        before << {"cmd" => "printf '%s' '{{ .Values.rendered.#{name} | toString | b64enc }}' | base64 -d > '#{File.join(work, name)}'", "enableTemplating" => true}
      end
    end
    before << {"cmd" => "printf '%s\\n' '#{item.fetch('name')}' >> '#{marker}'"}
    selected.merge("actions" => {"onDeploy" => {"before" => before}})
  end
  fixture = {
    "apiVersion" => definition.fetch("apiVersion"), "kind" => definition.fetch("kind"),
    "metadata" => {"name" => "conduit-operational-selection", "version" => "0.0.0"},
    "values" => definition.fetch("values"), "components" => fixture_components
  }
  File.write(File.join(work, "zarf.yaml"), YAML.dump(fixture))
  FileUtils.cp(File.join(directory, "values.yaml"), work)
  FileUtils.cp(File.join(directory, "values.schema.json"), work)
  FileUtils.mkdir_p(File.join(work, "home"))
  env = {"HOME" => File.join(work, "home"), "PATH" => ENV.fetch("PATH"),
         "KUBECONFIG" => File.join(work, "no-cluster-config"), "ZARF_NO_PROGRESS" => "true", "TMPDIR" => ENV.fetch("TMPDIR", work)}
  output, errors, created = Open3.capture3(env, zarf, "package", "create", work,
    "--confirm", "--skip-sbom", "--architecture", "amd64", "--output", work,
    "--cache", File.join(work, "cache"), "--no-color", unsetenv_others: true)
  abort "action-only package create failed:\n#{output}\n#{errors}" unless created.success?
  archive = Dir[File.join(work, "zarf-package-conduit-operational-selection-*.tar.zst")].fetch(0)

  run_case = lambda do |name, choice, variables, success, expected, before_any_action = false|
    FileUtils.rm_f(marker)
    values = {"ENVIRONMENT_ID" => "fixture-environment", "AGENT_ID" => "fixture-agent"}.merge(variables)
    values_file = File.join(work, "case-values.yaml")
    File.write(values_file, JSON.generate(values))
    flags = ["--values", values_file]
    flags += ["--components", choice] unless choice.nil?
    stdout, stderr, result = Open3.capture3(env, zarf, "package", "deploy", archive,
      "--confirm", "--connected", "--cache", File.join(work, "cache"), "--no-color",
      *flags, unsetenv_others: true)
    selected = File.exist?(marker) ? File.readlines(marker, chomp: true) : []
    abort "#{name}: unexpected exit:\n#{stdout}\n#{stderr}" unless result.success? == success
    if success
      abort "#{name}: selected #{selected.inspect}, expected #{expected.inspect}" unless selected == expected
    else
      abort "#{name}: refusal lost its cause:\n#{stdout}\n#{stderr}" unless (stdout + stderr).downcase.include?(expected.downcase)
      abort "#{name}: ran an action before refusing" if before_any_action && !selected.empty?
      abort "#{name}: reached the disabled metrics component" if selected.include?("uds-metrics")
    end
    cases += 1
  end
  run_case.call("baseline --confirm", nil, {}, true, %w[runtime])
  run_case.call("plain UDS", "uds", {}, true, %w[runtime uds])
  run_case.call("metrics UDS", "uds", {"UDS_METRICS" => true, "REPOSITORY_KIND" => "postgres", "METRICS_ADDRESS" => ":9090"}, true, %w[runtime uds])
  run_case.call("webhook Secret", nil, {"REPOSITORY_KIND" => "postgres", "NOTIFICATION_TRANSPORT" => "webhook",
    "NOTIFICATION_WEBHOOK_SECRET_NAME" => "fixture-webhook", "NOTIFICATION_WEBHOOK_SECRET_KEY" => "endpoint"}, true, %w[runtime])
  webhook = YAML.safe_load_file(File.join(work, "HUB_OPERATIONAL_ENV")).find { |item| item["name"] == "NOTIFICATION_WEBHOOK_URL" }
  abort "Zarf action lost the required webhook Secret reference" unless webhook == {
    "name" => "NOTIFICATION_WEBHOOK_URL", "valueFrom" => {"secretKeyRef" => {"name" => "fixture-webhook", "key" => "endpoint", "optional" => false}}
  }
  sender = 'Conduit "Review" <ops@example.test>'
  run_case.call("native, Plan and preparer", nil, {"REPOSITORY_KIND" => "postgres", "NOTIFICATION_TRANSPORT" => "native",
    "NOTIFICATION_SMTP_ADDRESS" => "smtp.example.test:465", "NOTIFICATION_SMTP_FROM" => sender,
    "NOTIFICATION_SMTP_CREDENTIALS_SECRET_NAME" => "fixture-smtp", "NOTIFICATION_SLACK_TOKEN_SECRET_NAME" => "fixture-slack",
    "PLAN_ORCHESTRATOR" => "temporal", "PLAN_TEMPORAL_ADDRESS" => "plan.example.test:7233",
    "MATERIAL_PREPARER_COMMAND" => "/opt/material/prepare", "MATERIAL_PREPARER_TOOLS_PVC" => "fixture-material",
    "MATERIAL_PREPARER_TOOLS_MOUNT_PATH" => "/opt/material"}, true, %w[runtime])
  generated = YAML.safe_load_file(File.join(work, "HUB_OPERATIONAL_ENV")).to_h { |item| [item["name"], item["value"]] }
  abort "Zarf action changed quoted sender metadata" unless generated["NOTIFICATION_SMTP_FROM"] == sender
  abort "Zarf action lost Plan frontend binding" unless generated["PLAN_ORCHESTRATOR"] == "temporal" && generated["PLAN_TEMPORAL_ADDRESS"] == "plan.example.test:7233"
  abort "Zarf action lost native file paths" unless generated["NOTIFICATION_SMTP_PASSWORD_FILE"] == "/run/notification-smtp/password" && generated["NOTIFICATION_SLACK_TOKEN_FILE"] == "/run/notification-slack/token"
  mounts = YAML.safe_load_file(File.join(work, "SPOKE_OPERATIONAL_MOUNTS"))
  volumes = YAML.safe_load_file(File.join(work, "SPOKE_OPERATIONAL_VOLUMES"))
  abort "Zarf action lost the read-only preparer PVC" unless
    mounts == [{"name" => "material-preparer-tools", "mountPath" => "/opt/material", "readOnly" => true}] &&
    volumes == [{"name" => "material-preparer-tools", "persistentVolumeClaim" => {"claimName" => "fixture-material", "readOnly" => true}}]
  # Valid Unicode is metadata, not whitespace to normalize. Exercise quotes,
  # a backslash, composed/decomposed letters, CJK, emoji and nonbreaking spaces.
  unicode_sender = "\"Jos\u00e9 \\\"Ops\\\" \\\\ Team \u2014 \u6771\u4eac\u00a0Crew\u202fCafe\u0301\u{1f680}\" <ops@example.test>"
  run_case.call("Unicode sender bytes", nil, {"REPOSITORY_KIND" => "postgres", "NOTIFICATION_TRANSPORT" => "native",
    "NOTIFICATION_SMTP_ADDRESS" => "smtp.example.test:465", "NOTIFICATION_SMTP_FROM" => unicode_sender}, true, %w[runtime])
  rendered_sender = YAML.safe_load_file(File.join(work, "HUB_OPERATIONAL_ENV")).find { |item| item["name"] == "NOTIFICATION_SMTP_FROM" }.fetch("value")
  abort "Zarf/YAML changed valid Unicode sender bytes" unless rendered_sender.bytes == unicode_sender.bytes
  run_case.call("retired UDS component", "uds-metrics", {}, false, "uds-metrics", true)
  run_case.call("observability without metrics", "uds-observability", {}, false, "uds-observability requires UDS_METRICS=true")
  run_case.call("metrics without PostgreSQL", "uds", {"UDS_METRICS" => true, "METRICS_ADDRESS" => ":9090"}, false, "UDS_METRICS requires REPOSITORY_KIND=postgres and METRICS_ADDRESS=:9090")
  run_case.call("metrics without listener", "uds", {"UDS_METRICS" => true, "REPOSITORY_KIND" => "postgres"}, false, "UDS_METRICS requires REPOSITORY_KIND=postgres and METRICS_ADDRESS=:9090")
  run_case.call("notifications with file store", nil, {"NOTIFICATION_TRANSPORT" => "log"}, false, "notifications require REPOSITORY_KIND=postgres")
  run_case.call("webhook without Secret", nil, {"REPOSITORY_KIND" => "postgres", "NOTIFICATION_TRANSPORT" => "webhook"}, false, "webhook requires NOTIFICATION_WEBHOOK_SECRET_NAME")
  run_case.call("native without channel", nil, {"REPOSITORY_KIND" => "postgres", "NOTIFICATION_TRANSPORT" => "native"}, false, "native notifications require SMTP address or Slack token Secret")
  run_case.call("SMTP without sender", nil, {"REPOSITORY_KIND" => "postgres", "NOTIFICATION_TRANSPORT" => "native", "NOTIFICATION_SMTP_ADDRESS" => "smtp.example.test:465"}, false, "SMTP requires NOTIFICATION_SMTP_FROM")
  run_case.call("Plan frontend absent", nil, {"PLAN_ORCHESTRATOR" => "temporal"}, false, "temporal Plan orchestration requires PLAN_TEMPORAL_ADDRESS")
  run_case.call("preparer traversal", nil, {"MATERIAL_PREPARER_COMMAND" => "/opt/../prepare"}, false, "material preparer command must be a clean absolute path")
  {
    "NOTIFICATION_TRANSPORT" => "invalid", "NOTIFICATION_WEBHOOK_TIMEOUT" => "0s",
    "NOTIFICATION_SMTP_CREDENTIALS_SECRET_NAME" => 'invalid"name',
    "NOTIFICATION_SMTP_FROM" => "bad\nsender@example.test", "MATERIAL_PREPARER_COMMAND" => "relative/prepare"
  }.each do |name, value|
    run_case.call("pattern #{name}", nil, {name => value}, false, "does not match", true)
  end

  # Every operational value is tested through Zarf's actual Go-regexp admission,
  # not a Ruby approximation of its Unicode character classes. Each template is
  # valid with the inserted character absent, so format errors cannot mask it.
  control_templates = {
    "NOTIFICATION_SMTP_FROM" => "Conduit%sOps <ops@example.test>",
    "NOTIFICATION_SMTP_ADDRESS" => "smtp%s.example.test:465",
    "PLAN_TEMPORAL_ADDRESS" => "plan%s.example.test:7233",
    "NOTIFICATION_TRANSPORT" => "web%shook",
    "NOTIFICATION_WEBHOOK_SECRET_NAME" => "fixture%s-webhook",
    "NOTIFICATION_WEBHOOK_SECRET_KEY" => "end%spoint",
    "NOTIFICATION_WEBHOOK_TIMEOUT" => "1%s2s",
    "NOTIFICATION_NATIVE_TIMEOUT" => "1%s2s",
    "NOTIFICATION_SMTP_TLS_MODE" => "start%stls",
    "NOTIFICATION_SMTP_CREDENTIALS_SECRET_NAME" => "fixture%s-smtp",
    "NOTIFICATION_SMTP_USERNAME_KEY" => "user%sname",
    "NOTIFICATION_SMTP_PASSWORD_KEY" => "pass%sword",
    "NOTIFICATION_SMTP_CA_CONFIGMAP_NAME" => "fixture%s-ca",
    "NOTIFICATION_SMTP_CA_KEY" => "ca%s.pem",
    "NOTIFICATION_SLACK_TOKEN_SECRET_NAME" => "fixture%s-slack",
    "NOTIFICATION_SLACK_TOKEN_KEY" => "bot%stoken",
    "PLAN_ORCHESTRATOR" => "tem%sporal",
    "MATERIAL_PREPARER_COMMAND" => "/opt/%sprepare",
    "MATERIAL_PREPARER_TOOLS_PVC" => "fixture%s-material",
    "MATERIAL_PREPARER_TOOLS_MOUNT_PATH" => "/opt/%smaterial"
  }
  abort "Unicode audit must cover every operational variable" unless control_templates.keys.sort == operational_defaults.keys.sort
  codepoints = (1..0x1f).to_a + (0x7f..0x9f).to_a + [0x2028, 0x2029, 0xfffe, 0xffff]
  control_templates.each do |name, template|
    codepoints.each do |codepoint|
      value = format(template, [codepoint].pack("U"))
      run_case.call(format("%s U+%04X", name, codepoint), nil, {name => value}, false, "does not match", true)
    end
  end
  # Structured values files also carry NUL, which cannot occur in argv.
  # The same native schema must reject it before any action executes.
  control_templates.each do |name, template|
    run_case.call("NUL #{name}", nil, {name => format(template, "\0")}, false, "does not match", true)
  end

end
puts "Pinned Zarf create/select and deploy guards passed #{cases} action-only cases; no cluster or scrape exercised"
