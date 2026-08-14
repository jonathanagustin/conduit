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
  File.read(path)
end

workflow = read_workflow.call("images.yml")
ci_workflow = read_workflow.call("ci.yml")
hub_containerfile = File.read(File.expand_path("../Containerfile", __dir__))
agent_containerfile = File.read(File.expand_path("../Containerfile.agent", __dir__))
e2e_workflow = read_workflow.call("e2e.yml")
docker_ignore = File.read(File.expand_path("../.dockerignore", __dir__))
container_ignore = File.read(File.expand_path("../.containerignore", __dir__))

cosign_action = "sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6"
retry_wrapper = "./scripts/retry-keyless-signing.sh"
abort "Cosign installer must be pinned for image and runtime signing" unless workflow.scan(cosign_action).length == 2
abort "Cosign release must be pinned" unless workflow.scan("cosign-release: v3.1.2").length == 2
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
abort "Grype database cache action must be commit pinned" unless workflow.scan("actions/cache@27d5ce7f107fe9357f9df03efb73ab90386fccae").length == 2
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
  [hub_containerfile, agent_containerfile].all? { |containerfile| containerfile.include?("golang:1.26.5-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651") }
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
