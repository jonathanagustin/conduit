#!/usr/bin/env ruby
# frozen_string_literal: true

require "yaml"

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
cosign_build_mod = File.read(File.expand_path("../tools/cosign-build/go.mod", __dir__))
cosign_acr_mod = File.read(File.expand_path("../tools/cosign-build/compat/acr/go.mod", __dir__))
cosign_acr_adapter = File.read(File.expand_path("../tools/cosign-build/compat/acr/pkg/credhelper/helper.go", __dir__))
e2e_workflow = read_workflow.call("e2e.yml")
docker_ignore = File.read(File.expand_path("../.dockerignore", __dir__))
container_ignore = File.read(File.expand_path("../.containerignore", __dir__))

cosign_action = "sigstore/cosign-installer@756fe2d284c7292c4e9b62c8a28f1e5629c59532"
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
cosign_release = "v3.1.4"
clean_cluster = File.read(File.expand_path("test-gitless-clean-cluster.sh", __dir__))
optimizer_release_workflow = read_workflow.call("optimizer-candidate-release.yml")
retry_wrapper = "./scripts/retry-keyless-signing.sh"
# Nothing asserted the trigger shape of this workflow, which is the one that
# actually signs and publishes: images, the Flux runtime source, the Helm
# charts, the Zarf packages and the CLI. ADR 0006 named the optimizer-release
# and Gitless-acceptance checks as holding publication to `workflow_dispatch`,
# but neither of those reads this file, so adding `on: push:` here passed every
# gate in the repository while breaking ADR 0001's manual-publication rule and
# assumption A5.
#
# Asserted as an exact key set, the stronger form already used by
# verify-optimizer-candidate-release-workflow.rb, rather than as a scan for
# known-bad trigger names: a name list only rejects the triggers whoever wrote
# it thought of, while the exact set rejects any trigger that is ADDED.
# Psych implements YAML 1.1 and decodes the unquoted GitHub `on` key as the
# boolean true, so both parser representations are accepted before the keys are
# compared.
workflow_document = YAML.safe_load(workflow, aliases: true)
publication_triggers = workflow_document.is_a?(Hash) ? (workflow_document["on"] || workflow_document[true]) : nil
abort "publication workflow must have exactly one workflow_dispatch trigger (ADR 0001, A5)" unless
  publication_triggers.is_a?(Hash) && publication_triggers.keys == ["workflow_dispatch"]
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
abort "Hub Cosign source build must pin the reviewed upstream release" unless
  cosign_build_mod.include?("tool github.com/sigstore/cosign/v3/cmd/cosign") &&
    cosign_build_mod.match?(/^\s*github\.com\/sigstore\/cosign\/v3 v3\.1\.4 \/\/ indirect$/)
abort "Hub Cosign source build must pin the patched Go toolchain and dependency graph" unless
  cosign_build_mod.match?(/^go 1\.27\.4$/) &&
    cosign_build_mod.match?(/^\s*golang\.org\/x\/mod v0\.41\.0 \/\/ indirect$/) &&
    cosign_build_mod.match?(/^\s*golang\.org\/x\/text v0\.43\.0 \/\/ indirect$/) &&
    cosign_build_mod.match?(/^\s*google\.golang\.org\/grpc v1\.84\.0 \/\/ indirect$/)
abort "Cosign must replace the vulnerable legacy Azure helper with the maintained compatibility adapter" unless
  cosign_build_mod.include?("replace github.com/chrismellard/docker-credential-acr-env => ./compat/acr") &&
    cosign_acr_mod.match?(/^\s*github\.com\/osscontainertools\/docker-credential-acr v1\.0\.1$/) &&
    cosign_acr_adapter.include?("return maintained.NewACRCredentialsHelper()")
abort "Cosign must retain the compression security update" unless
  cosign_build_mod.match?(/^\s*github\.com\/klauspost\/compress v1\.20\.3 \/\/ indirect$/)
abort "Hub build context must include the Azure helper compatibility source" unless
  hub_containerfile.include?("COPY tools/cosign-build/compat ./compat") &&
    [docker_ignore, container_ignore].all? do |ignore|
      ignore.include?("!tools/cosign-build/compat/acr/go.mod") &&
        ignore.include?("!tools/cosign-build/compat/acr/pkg/credhelper/*.go")
    end
abort "CI must exercise the actual Azure credential helper host boundary" unless
  ci_workflow.include?("./scripts/test-cosign-credential-helper.sh") &&
    File.executable?(File.expand_path("test-cosign-credential-helper.sh", __dir__))
abort "Hub runtime must build and carry the locked Cosign command" unless
  hub_containerfile.include?("COPY tools/cosign-build/go.mod tools/cosign-build/go.sum ./") &&
    hub_containerfile.include?("go mod download") &&
    hub_containerfile.include?("-o /out/cosign github.com/sigstore/cosign/v3/cmd/cosign") &&
    hub_containerfile.include?("COPY --from=build --chown=65532:65532 --chmod=0755 /out/cosign /usr/local/bin/cosign")
abort "Hub runtime must not fall back to a vulnerable prebuilt Cosign binary" if
  hub_containerfile.include?("sigstore/cosign/releases/download") ||
    hub_containerfile.include?("ARG COSIGN_VERSION=")
abort "no cosign-release may be left unpinned" unless
  (workflow + optimizer_release_workflow).scan(/cosign-release: (\S+)/).flatten.uniq == [cosign_release]
abort "image and runtime jobs require GitHub OIDC" unless workflow.scan("id-token: write").length == 2
abort "keyless signing retry wrapper must be behaviorally tested" unless
  File.executable?(File.expand_path("test-retry-keyless-signing.sh", __dir__))
# The guarantee is that signing binds the immutable DIGEST rather than a tag,
# which is what makes the signature unforgeable by a later re-tag. It is not a
# claim about what the package is called: matching `platform-<component>` here
# pinned an unrelated naming choice, and broke the moment the images moved to a
# repository-named package so GHCR would link them to this repository at all.
#
# Matched as ONE expression rather than two independent `include?`s. The pair
# was satisfiable by a digest reference anywhere in the file -- the attest and
# syft steps both carry one -- so re-pointing the sign call at `:latest` passed
# the gate. It is the sign call that has to name the digest, so that is what is
# asserted.
abort "immutable image digest is not signed" unless
  workflow.match?(/#{Regexp.escape(retry_wrapper)} cosign sign --yes\s+'[^']*@\$\{\{ steps\.build\.outputs\.digest \}\}'/)
abort "immutable runtime digest is not signed" unless workflow.include?("#{retry_wrapper} cosign sign --yes \"${artifact_reference}\"")
abort "runtime artifact must publish to a component tag in the linked Conduit package" unless
  workflow.include?('"oci://ghcr.io/${GITHUB_REPOSITORY}:runtime-${version}"') &&
    workflow.include?('artifact_reference="ghcr.io/${GITHUB_REPOSITORY}:runtime-${version}@${artifact_digest}"')
abort "Helm charts must publish below the linked Conduit repository path" unless
  workflow.include?('helm push "${package}" "oci://ghcr.io/${GITHUB_REPOSITORY}"') &&
    workflow.include?('reference="ghcr.io/${GITHUB_REPOSITORY}/${chart}:${version}@${digest}"')
# The two assertions above name the runtime artifact and the charts because
# those are the two that broke (#185). This one is the property they are both
# instances of, stated once so the NEXT artifact kind cannot reintroduce the
# defect in a third place: EVERY GHCR coordinate this workflow publishes to or
# reads back must live in the package named for this repository.
#
# That is not a style preference, it is the only thing that makes the push work.
# GHCR links a package to a repository when the package is CREATED, and a
# package named for the repository is linked on creation; anything else is
# linked to whoever created it -- here, the monorepo this code was extracted
# from. A repository's GITHUB_TOKEN holds `packages: write` only for packages
# linked to that repository, so an unlinked name is refused. #154 hit that on
# the images, and because the refusal is a 403 on a blob HEAD it arrives after
# the multi-arch build, the SBOM and the signature have all succeeded, reading
# like a token-scope problem rather than a naming one. #157 then hit exactly the
# same wall in the two places #154 had not reached, at the cost of a tagged
# release (v0.17.0) that published two images and no runtime artifact, no
# charts, no Zarf packages, no CLI and an empty asset list.
#
# Asserted by exhaustion rather than by a list of known-bad names, for the
# reason the trigger check above states: a name list only rejects the
# coordinates whoever wrote it thought of, while requiring every coordinate to
# be the linked one rejects any coordinate that is ADDED. Substituting the two
# spellings of the repository first (`${{ github.repository }}` in Actions
# expressions, `${GITHUB_REPOSITORY}` in shell) means `${{
# github.repository_owner }}` and `${GITHUB_REPOSITORY_OWNER}` -- the two
# spellings that actually caused #154 and #185 -- do not substitute and are
# reported. The bare `helm registry login ghcr.io` names the registry rather
# than a package and carries no slash, so it is not a coordinate and is not
# matched.
#
# Scoped to the publication workflow deliberately.
# platform-optimizer-candidate-release.yml still pushes to
# `ghcr.io/${GITHUB_REPOSITORY_OWNER}/platform-optimizer-<backend>`, which is
# the same unlinked shape and will meet the same 403 on its first dispatch. It
# is left alone here because it has never published anything, so widening this
# gate would paint main red over a hypothetical rather than over a regression,
# and because relocating a protected-environment publication path is a release
# decision rather than a drive-by.
linked_package = "ghcr.io/CONDUIT-LINKED-PACKAGE"
foreign_coordinates = workflow
  .gsub("ghcr.io/${{ github.repository }}", linked_package)
  .gsub("ghcr.io/${GITHUB_REPOSITORY}", linked_package)
  .lines.each_with_index
  .filter_map { |line, index| "line #{index + 1}: #{line.strip}" if line.gsub(linked_package, "").include?("ghcr.io/") }
abort "publication must target only the repository-named GHCR package GHCR will link " \
      "(#154, #185):\n  #{foreign_coordinates.join("\n  ")}" unless foreign_coordinates.empty?
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
  [hub_containerfile, agent_containerfile].all? { |containerfile| containerfile.include?("golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea") }
abort "runtime roots must use the reviewed Chainguard static index by exact digest" unless
  [hub_containerfile, agent_containerfile].all? { |containerfile| containerfile.include?("cgr.dev/chainguard/static:latest@sha256:fc42af33e643f20d08baf03cddcc1c02ba57052dfb02edb0d79f3fc47bcea8d9") }
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
