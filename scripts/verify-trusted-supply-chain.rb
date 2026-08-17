#!/usr/bin/env ruby
# frozen_string_literal: true

# The workflows live at the repository root, not beside this tree: platform is a
# subtree of a larger repository and GitHub only reads .github/workflows from the
# root, which is also why they carry a platform- prefix there.
platform_root = File.expand_path("..", __dir__)
workflow_root = File.expand_path("..", platform_root)
workflow_dir = File.join(workflow_root, ".github", "workflows")
workflow_dir = File.join(platform_root, ".github", "workflows") unless File.directory?(workflow_dir)
read_workflow = lambda do |name|
  path = File.join(workflow_dir, "platform-#{name}")
  path = File.join(workflow_dir, name) unless File.exist?(path)
  # UTF-8 explicitly, as the node-version scan below already does. Without it
  # Ruby reads under the runner's default external encoding, which is US-ASCII
  # in CI, and the first `scan` against a workflow containing any non-ASCII
  # character dies with "invalid byte sequence in US-ASCII". That is not this
  # gate refusing something: it is the gate crashing before it checks anything,
  # and platform-ci.yml already contains such characters, so the next comment
  # added to a workflow this reads would have been the one to trip it.
  File.read(path, encoding: "UTF-8")
end

workflow = read_workflow.call("images.yml")
ci_workflow = read_workflow.call("ci.yml")
hub_containerfile = File.read(File.expand_path("../Containerfile", __dir__))
agent_containerfile = File.read(File.expand_path("../Containerfile.agent", __dir__))
e2e_workflow = read_workflow.call("e2e.yml")
docker_ignore = File.read(File.expand_path("../.dockerignore", __dir__))
container_ignore = File.read(File.expand_path("../.containerignore", __dir__))

cosign_action = "sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6"
# Declared once, like cosign_action above, rather than written into the
# assertion. The old form scanned for a literal "cosign-release: v3.1.2", which
# guarded the two occurrences in images.yml and silently ignored the third in
# platform-optimizer-candidate-release.yml -- so the signing toolchain could
# already drift between the workflows that sign images and the one that signs
# optimizer candidates, and nothing would say so.
#
# The point of the pin is that an unattended bot cannot change the signing
# toolchain on its own: Renovate rewrites workflows wholesale and cannot satisfy
# a gate that names the old value, so the change becomes a red gate a person has
# to resolve rather than a hunk in a grouped dependency PR. That property is
# kept. What is added is that the version is now tethered to the one cosign this
# repository pins by sha256 and actually executes -- the clean-cluster Gitless
# acceptance download -- so a bump has to move the checksum too, and the tag
# pin stops being the weakest link on its own.
cosign_release = "v3.1.3"
clean_cluster = File.read(File.expand_path("test-gitless-clean-cluster.sh", __dir__))
optimizer_release_workflow = read_workflow.call("optimizer-candidate-release.yml")
retry_wrapper = "./scripts/retry-keyless-signing.sh"
abort "Cosign installer must be pinned for image and runtime signing" unless workflow.scan(cosign_action).length == 2
abort "Cosign release must be pinned for image and runtime signing" unless
  workflow.scan("cosign-release: #{cosign_release}").length == 2
abort "Cosign release must be pinned for optimizer candidate signing" unless
  optimizer_release_workflow.scan("cosign-release: #{cosign_release}").length == 1
# The signing workflows and the acceptance suite have to run the same cosign, or
# the version the suite proves out by checksum is not the version that signs
# what ships.
abort "Cosign release must match the checksum-pinned download in test-gitless-clean-cluster.sh" unless
  clean_cluster.include?("/sigstore/cosign/releases/download/#{cosign_release}/")
abort "no cosign-release may be left unpinned" unless
  (workflow + optimizer_release_workflow).scan(/cosign-release: (\S+)/).flatten.uniq == [cosign_release]
abort "image and runtime jobs require GitHub OIDC" unless workflow.scan("id-token: write").length == 2
abort "keyless signing retry wrapper must be behaviorally tested" unless
  File.executable?(File.expand_path("test-retry-keyless-signing.sh", __dir__))
abort "immutable image digest is not signed" unless workflow.include?("#{retry_wrapper} cosign sign --yes") &&
  workflow.include?("platform-${{ matrix.component }}@${{ steps.build.outputs.digest }}")
abort "immutable runtime digest is not signed" unless workflow.include?("#{retry_wrapper} cosign sign --yes \"${artifact_reference}\"")
abort "runtime descriptor must bind the expected keyless signer" unless
  workflow.include?('certificate_issuer="https://token.actions.githubusercontent.com"') &&
    workflow.include?('certificate_identity="https://github.com/${GITHUB_REPOSITORY}/.github/workflows/platform-images.yml@${GITHUB_REF}"') &&
    workflow.include?("node scripts/render-runtime-source-descriptor.mjs") &&
    File.executable?(File.expand_path("render-runtime-source-descriptor.mjs", __dir__)) &&
    File.executable?(File.expand_path("render-runtime-source-descriptor.test.mjs", __dir__))
abort "Release BuildKit provenance must remain enabled without breaking PR --load" unless
  workflow.include?("provenance: ${{ github.event_name != 'pull_request' && 'mode=max' || 'false' }}")
abort "Release BuildKit SBOM must remain enabled without breaking PR --load" unless
  workflow.include?("sbom: ${{ github.event_name != 'pull_request' }}")
abort "Syft must be version and archive-digest pinned" unless workflow.include?("v1.50.0/syft_1.50.0_linux_amd64.tar.gz") &&
  workflow.include?("bf7b29ff57f06da30918266a0e1c2885a8f99784798d1bdb1628886aa015d788")
abort "Grype must be version and archive-digest pinned" unless workflow.include?("v0.116.1/grype_0.116.1_linux_amd64.tar.gz") &&
  workflow.include?("0122df7b655981abe547ad3d2190d65551dac6a2bfc80b4dc2a989b5d0587458")
# Asserted as a property rather than as a literal SHA: what matters is that both
# occurrences are commit-pinned and identical, which a bot bumping the action can
# satisfy honestly. Naming one commit made every routine bump a verifier edit,
# and a control that cries wolf on safe changes is one people learn to retarget
# without reading.
# mise.toml is the single source of truth for tool versions, and the workflows
# name their own. Nothing reconciled them, so a bot could move CI to one Node
# while every local gate -- and verify-local.sh, which claims to run what CI runs
# -- stayed on another. Same failure the VERSION/tag check exists for, and the
# same fix: assert agreement rather than hope.
mise = File.read(File.expand_path("../mise.toml", __dir__))
declared_node = mise[/^node = "([^"]+)"/, 1]
abort "mise.toml declares no node version" if declared_node.nil?
workflow_nodes = Dir[File.join(workflow_dir, "*.yml")]
  .flat_map { |path| File.read(path, encoding: "UTF-8").scan(/node-version: (\S+)/).flatten }
  .uniq
abort "workflow node-version #{workflow_nodes.reject { |v| v == declared_node }.join(", ")} " \
      "disagrees with mise.toml #{declared_node}" unless
  workflow_nodes.all? { |version| version == declared_node }

cache_pins = workflow.scan(%r{actions/cache@([0-9a-f]{40})}).flatten
abort "Grype database cache action must be commit pinned twice and identically" unless
  cache_pins.length == 2 && cache_pins.uniq.length == 1
abort "Grype database must be updated once, restored exactly, and not changed during image scans" unless
  workflow.include?("timeout 10m /tmp/grype db update") && workflow.include?("fail-on-cache-miss: true") &&
    workflow.include?('GRYPE_DB_AUTO_UPDATE: "false"')
abort "Syft must emit native and SPDX SBOM evidence for both release architectures" unless
  workflow.include?('architectures+=(arm64)') && workflow.include?('--platform "linux/${architecture}"') &&
    workflow.include?('syft-json=${native_sbom}') && workflow.include?('spdx-json=${spdx_sbom}')
abort "Grype must scan each Syft SBOM and fail the release on high findings" unless workflow.include?('"sbom:${native_sbom}"') &&
  workflow.include?("--fail-on high") && workflow.include?("timeout 10m")
abort "SBOM and vulnerability evidence must be attached to the immutable image" unless
  workflow.include?("#{retry_wrapper} cosign attest --yes --predicate 'dist/${{ matrix.component }}-'") &&
    workflow.include?("https://platform.delivery/attestations/grype/v1")
abort "Helm release charts must be digest-signed" unless workflow.include?("Package, publish, and sign Helm runtime charts") &&
  workflow.include?("#{retry_wrapper} cosign sign --yes \"${reference}\"")
abort "Helm release charts must carry SPDX SBOMs" unless workflow.include?("scripts/generate-helm-sbom.rb") &&
  workflow.include?('"${package}.spdx.json"')
abort "Conduit release must build the supported OS/architecture matrix" unless
  workflow.include?("linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64") &&
    workflow.include?('CGO_ENABLED=0 GOOS="${os}" GOARCH="${architecture}" go build')
abort "Conduit archives must be reproducible and checksum-indexed" unless
  workflow.include?('tar --sort=name --mtime="@${epoch}" --owner=0 --group=0 --numeric-owner') &&
    workflow.include?("gzip -n") &&
    workflow.include?("sha256sum conduit_*.tar.gz conduit_*.zip | sort -k2 > ../SHA256SUMS")
abort "Conduit archives must carry SPDX SBOMs and keyless signatures" unless
  workflow.include?('spdx-json=cli-dist/sbom/conduit_${version}_${os}_${architecture}.spdx.json') &&
    workflow.include?("#{retry_wrapper} cosign sign-blob --yes --bundle \"${archive}.sigstore.json\"")
abort "Zarf and every Cosign keyless operation must use bounded retries" unless
  workflow.include?("#{retry_wrapper} /tmp/zarf package sign") &&
    workflow.scan("#{retry_wrapper} cosign").length == 7
abort "Conduit archives must have verified keyless SLSA provenance bundles" unless
  workflow.include?("#{retry_wrapper} cosign attest-blob --yes") &&
    workflow.include?('--type slsaprovenance1') &&
    workflow.include?('cosign verify-blob-attestation') &&
    workflow.include?('--certificate-identity "${builder}"') &&
    workflow.include?('cli-dist/provenance/*')
abort "release must not depend on unavailable private-repository GitHub attestations" if
  workflow.include?("actions/attest@") || workflow.include?("attestations: write")
abort "runtime builds must use the fixed Go toolchain by exact image digest" unless
  [hub_containerfile, agent_containerfile].all? { |containerfile| containerfile.include?("golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36") }
abort "runtime roots must use the reviewed Chainguard static index by exact digest" unless
  [hub_containerfile, agent_containerfile].all? { |containerfile| containerfile.include?("cgr.dev/chainguard/static:latest@sha256:399c8cb4858f05aaa33f43f02a2e75f28d40f016c0f86e5ba6075769e3303791") }
abort "Helm must be rebuilt from integrity-pinned 4.2.3 source with fixed oras-go" unless
  agent_containerfile.include?("sha256:9ca555fa3777082fd646c95abf4ee17fc31e25ba97bad76da9de1df2332be168") &&
    agent_containerfile.include?("oras.land/oras-go/v2@v2.6.2")
abort "kubectl must be rebuilt from integrity-pinned 1.36.3 source with fixed network modules" unless
  agent_containerfile.include?("sha256:c3144dc4c33a1d597422254e21c4143cbad78795271d6d61ddb3c71596320878") &&
    agent_containerfile.include?("golang.org/x/net@v0.56.0") && agent_containerfile.include?("golang.org/x/text@v0.39.0")
abort "Helm 3 compatibility must be explicit while Helm 4 uses the hardened build" unless
  e2e_workflow.include?('use-upstream: "true"') && e2e_workflow.include?('use-upstream: "false"')
abort "Docker and Podman build contexts must exclude the same files" unless docker_ignore == container_ignore
abort "Containerfile linting must use integrity-pinned Hadolint 2.15.1 and fail at info" unless
  ci_workflow.include?("hadolint/releases/download/v2.15.1/hadolint-linux-x86_64") &&
    ci_workflow.include?("c7187db94eeeeca956519a6af171adc31453941a1e777961f6e680f697c8c507") &&
    File.read(File.expand_path("verify-containerfiles.sh", __dir__)).include?("--failure-threshold info")

puts "Trusted OCI supply-chain contract verified"
