#!/usr/bin/env ruby
# frozen_string_literal: true

require "fileutils"
require "json"
require "open3"
require "tmpdir"
require "yaml"

root = File.expand_path("..", __dir__)
cases = 0
Dir.mktmpdir("conduit-lifecycle-command-") do |directory|
  tool = File.join(directory, "zarf")
  File.write(tool, <<~'RUBY')
    #!/usr/bin/env ruby
    require "json"
    args = ARGV
    abort "verification flag missing" unless args.include?("--verify=always")
    abort "signer changed" unless args[args.index("--certificate-identity").to_i + 1] == "fixture-signer"
    abort "issuer changed" unless args[args.index("--certificate-oidc-issuer").to_i + 1] == "fixture-issuer"
    abort "architecture changed" unless args[args.index("--architecture").to_i + 1] == "amd64"
    case args.take(2)
    when ["package", "pull"]
      directory = args.fetch(args.index("--output-directory") + 1)
      File.write(File.join(directory, "zarf-package-platform-runtime-amd64-0.1.0.tar.zst"), "fixture")
    when ["package", "inspect"]
      abort "signature rejected" if ENV["REFUSE_SIGNATURE"] == "1"
      puts File.read(ENV.fetch("DEFINITION"))
    when ["package", "deploy"]
      if ENV.fetch("API").end_with?("v1beta1")
        abort "native deployment used variables" if args.include?("--set-variables")
        file = args.fetch(args.index("--values") + 1)
        values = JSON.parse(File.read(file))
        abort "reviewed values changed" unless values == {"ENVIRONMENT_ID" => "lifecycle", "AGENT_ID" => "spoke-lifecycle"}
      else
        abort "legacy deployment used values file" if args.include?("--values")
        %w[ENVIRONMENT_ID=lifecycle AGENT_ID=spoke-lifecycle].each { |value| abort "lost variable" unless args.include?(value) }
      end
      abort "deployed a mutable registry tag" if args[2].start_with?("oci://")
      File.write(ENV.fetch("DEPLOY_MARKER"), args[2])
    else abort "unexpected command"
    end
  RUBY
  File.write(File.join(directory, "kubectl"), "#!/bin/sh\nexit 0\n")
  FileUtils.chmod(0o755, [tool, File.join(directory, "kubectl")])
  command = <<~'BASH'
    set -euo pipefail
    root=$1 work=$2 zarf=$3 architecture=amd64 namespace=platform-system issuer=fixture-issuer
    identity() { printf '%s' fixture-signer; }
    archive() { printf 'zarf-package-platform-runtime-amd64-%s.tar.zst' "$1"; }
    fail() { echo "$1" >&2; exit 1; }
    declare -A qualified_sources
    source "$root/scripts/lib/zarf-lifecycle-deploy.sh"
    deploy "$4" 0.1.0
    test "${qualified_sources[0.1.0]}" = "$(cat "$DEPLOY_MARKER")"
  BASH
  %w[zarf.dev/v1alpha1 zarf.dev/v1beta1].each do |api|
    %w[local registry invalid-version wrong-identity mutable-image signature-refusal unsupported-api].each do |scenario|
      work = File.join(directory, "#{api.split('/').last}-#{scenario}")
      FileUtils.mkdir_p(work)
      image = "ghcr.io/example/hub@sha256:#{'a' * 64}"
      image = "ghcr.io/example/hub:latest" if scenario == "mutable-image"
      definition = {"apiVersion" => api, "kind" => "ZarfPackageConfig", "metadata" => {"name" => "platform-runtime", "version" => "0.1.0", "annotations" => {"conduit.dev/hub-image" => image}}, "constants" => [{"name" => "HUB_IMAGE", "value" => image}]}
      definition["metadata"]["version"] = "0.2.0" if scenario == "invalid-version"
      definition["metadata"]["name"] = "other" if scenario == "wrong-identity"
      definition["apiVersion"] = "zarf.dev/v2" if scenario == "unsupported-api"
      path = File.join(work, "input.yaml")
      File.write(path, YAML.dump(definition))
      archive = File.join(work, "input.tar.zst")
      File.write(archive, "fixture")
      source = scenario == "registry" ? "oci://example/runtime:0.1.0" : archive
      marker = File.join(work, "deployed")
      env = {"PATH" => "#{directory}:#{ENV.fetch('PATH')}", "DEFINITION" => path, "API" => api, "DEPLOY_MARKER" => marker, "REFUSE_SIGNATURE" => scenario == "signature-refusal" ? "1" : "0"}
      out, error, status = Open3.capture3(env, "bash", "-c", command, "fixture", root, work, tool, source)
      expected = %w[local registry].include?(scenario)
      abort "#{api} #{scenario}: #{out}#{error}" unless status.success? == expected && File.exist?(marker) == expected
      cases += 1
    end
  end
end
puts "Lifecycle deploy commands pass #{cases} alpha/beta local/registry and refusal cases with stubbed tools; no cluster exercised"
