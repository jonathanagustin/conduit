# Ruby validators and third-party trust review

Review date: 2026-11-14. Tracked in [#598](https://github.com/jonathanagustin/conduit/issues/598).
This is a bounded provenance, dependency, advisory, and script-boundary review,
not a certification that every upstream source line is benign.

## Ruby ownership and execution boundaries

All 13 Ruby scripts present at the start of this review are Conduit-owned
validators or generators, not downloaded third-party `.rb` plugins. There is
no project Gemfile or gemspec. Their imports are supplied by the selected Ruby
runtime, which remains a third-party dependency with its own bundled gems.

| Scripts | Inputs and authority reviewed |
| --- | --- |
| `apollo-docs/validate-cli-fixtures.rb`, `validate-module-definitions.rb`, `validate-public-api.rb`, `verify-public-api-adapter.rb` | Local pinned snapshots, fixtures, and source; YAML/JSON decoding, no network calls or product execution. Module definitions can be supplied by the caller. |
| `verify-flux-runtime.rb`, `verify-helm-runtime.rb`, `verify-zarf-package.rb` | Local manifests and rendered charts; Helm subprocesses render, lint and package. Optional Zarf subprocesses lint. They do not install the runtime in a cluster. |
| `generate-helm-sbom.rb` | Reads a supplied chart archive with Ruby's TarReader and hashes entries; it does not extract them. Writes the supplied output path. |
| `verify-rubix-crds.rb` | Reads pinned JSON CRDs and their checksums. Setting `KUBECTL_BIN` explicitly enables server-side dry-run against a cluster; this review did not enable it. |
| `verify-trusted-supply-chain.rb`, `verify-release-workflow.rb`, `verify-release-preparation.rb`, `verify-optimizer-candidate-release-workflow.rb` | Source/AST policy checks against workflows, manifests and scripts. Fixed `git rev-parse` commands discover the checkout; no caller text is interpolated into those commands. These checks do not prove a live release or hosted Action run. |

Subprocess helpers use argument arrays rather than interpolating manifest values
into shell commands. Executables selected by the caller's PATH or environment
are still trusted execution inputs. The review checked imports, deserialization,
process calls, archive handling, file writes and network boundaries; it is not a
functional proof of all validators.

### Findings and corrections

* Four `.rb` validators used unrestricted YAML object decoding (ten call sites).
  An additional inline Ruby expression in `verify-apollo-routes.sh` did too.
  They now share `scripts/lib/safe_yaml.rb`: each stream document is decoded by
  Psych's restricted loader, with only Date/Time classes permitted. Local YAML
  anchors and multi-document streams remain supported. Tests reject Ruby object
  and symbol tags before initialization hooks run and cover timestamps, anchors,
  empty streams, null documents, and file loading. This is not a resource-limit
  sandbox for arbitrary huge or cyclic YAML; inputs remain controlled checkout
  fixtures and tool output.
* The local Ruby 3.4.12 runtime loaded `json` 2.9.1, affected by
  [GHSA-x2f5-4prf-w687](https://github.com/ruby/json/security/advisories/GHSA-x2f5-4prf-w687)
  (CVE-2026-54696). The affected API streams generated JSON to an IO object;
  the original validators use parsing or string generation followed by file
  writes, not that streaming API. This does not establish exploitation here.
  The active local runtime was upgraded to 2.19.9 and its 16 KiB streaming
  boundary checked after upgrading. The shared JSON import now rejects the
  advisory's affected range, `>=2.9.0,<2.19.9`, with an upgrade instruction.
  An inactive older default gem may still exist in a runner image; this is not
  an image-wide vulnerability-clearance claim.
* Helm SBOMs asserted `LicenseRef-Proprietary` without a declared project
  license. They now use SPDX `NOASSERTION`; the existing metadata document's
  CC0 data license does not grant a license to Conduit. The project licensing
  decision remains [#546](https://github.com/jonathanagustin/conduit/issues/546).

For a runner with an affected JSON version, install the upstream fixed gem
using that runner's Ruby/gem pair. The reviewed artifact is
[json-2.19.9.gem](https://rubygems.org/downloads/json-2.19.9.gem), SHA-256
`9b9025b7cdddafa38d316eca0b2358488e42d417045c1b90d216a9fefe46b79a`.
Verify that checksum before `ruby -S gem install --local <downloaded-file>
--no-document`, then check `ruby -rjson -e 'puts JSON::VERSION'` under the same
user and environment as validation. Native compilation may require a compiler;
the local runtime was updated alongside its default gem without restarting the
shared container. Validators do not download code themselves. Preserve upstream
Ruby/JSON license notices when distributing the runtime or gem.

## Dependency and toolchain checks

| Surface | Evidence and result | Limit |
| --- | --- | --- |
| Root Go module | `go mod verify`; `govulncheck` v1.9.0 against `./...`. Initially reported GO-2026-6860 and GO-2026-6866 for `x/net` v0.60.0, at module level without an imported-package/call trace. Updated to v0.62.0 and its required module versions; fresh scan reports no findings. | A clean known-advisory scan is not malicious-code detection. Build tools and optional paths need their own execution review. |
| Web lockfile | pnpm 12.18.0 audit: zero advisories in all severities; reported total 565 dependencies. | Registry advisory coverage and the reviewed lockfile only. Keep the 24-hour install quarantine and three-day Renovate delay. |
| Ruby runner libraries | OSV batch queries for 19 loaded gem versions: one match, JSON above. | Bundled libraries are not automatically safe because Ruby supplies them. This is not a full OS image scan. |
| Optimizer Rust lockfile | OSV batch queries for 23 registry crates: no matches. | Does not cover the C++/OS closure or qualify native artifacts. Native candidates remain UNADMITTED; HiGHS remains held by #590. |
| GitHub Actions | All 13 distinct remote Action repositories in workflows use full 40-character SHAs. GitHub resolves each exact commit under its named repository and reports a verified commit signature for each. Existing supply-chain policy checks pass. | Publisher identity/signature and immutable coordinates do not establish benign behavior, review nested dependencies, or prove hosted runtime compatibility. The #500 integration follow-up below distinguishes source review from hosted qualification. |
| pnpm executable | 12.18.0 publisher release artifact checksum and GitHub attestation verified against upstream release workflow/tag/source; modified archive rejected. | Upstream provenance uses a self-hosted runner; do not describe it as hosted-runner isolation. |
| Cosign | Separate locked source graph and maintained Azure helper adapter; see [source-build evidence](COSIGN-SOURCE-BUILD.md). | Not covered by the root `go mod vendor` command or root module scan. Retain separate binary/graph verification. |

Primary Go advisory records:
[GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860),
[GO-2026-6866](https://pkg.go.dev/vuln/GO-2026-6866).
JSON source and fix are published by [ruby/json](https://github.com/ruby/json),
with fixed release [v2.19.9](https://github.com/ruby/json/releases/tag/v2.19.9).
The checks above describe the versions and runtime actually examined on the
review date. Repeat them when lockfiles, compiler versions or runner images change.
Raw scanner responses remain outside the repository.

## Replace, fork, or vendor

Vendoring is feasible but transfers update and review responsibility to Conduit;
it does not remove upstream trust. A root `go mod vendor` preview produced 61
module entries, 2,666 files and 36,962,235 bytes (about 35.3 MiB), before the
separate Cosign graph. No vendor tree was committed by this review.

Small personal namespaces alone are not evidence of malicious code. Dependency
paths explain why these packages exist:

* `dprotaso/go-yit` enters through the OpenAPI generation tool, via the
  overlay/JSONPath stack. It is a build-tool dependency, not a justification to
  execute arbitrary runtime plugins.
* `agnivade/levenshtein` enters through gqlparser validation. A small bounded
  algorithm is a plausible replacement candidate if Conduit can remove the
  upstream requirement and test equivalent behavior.
* `facebookgo/clock` and the older `robfig/cron` module enter through Temporal's
  SDK; Conduit also directly uses cron v3. Replacing the direct scheduler does
  not remove Temporal's separate dependency.

Keep reviewed, maintained upstream implementations for YAML/JSON parsing,
cryptography, identity and database protocols. Do not replace these with casual
homegrown parsers or crypto. Prefer removing an unused dependency first; for a
necessary narrow helper, assess an owned adapter or licensed vendor copy with a
pinned upstream commit, preserved notices, focused tests and an update owner.
Porting these owned Ruby validators to the already pinned Go toolchain could
reduce the runner surface, but requires equivalent fixture and rejection tests;
this review does not claim that migration was performed.

## Actions major integration follow-up

Review date: 2026-11-14. #500 was merged independently while the administrator
status change was under local verification. Two validators still required the
previous checkout/setup-go commits. They now require the new exact pins; Go
1.27.4, credential persistence restrictions, manual publication and the existing
permission checks remain unchanged.

The GitHub commit API resolves all 18 selected coordinates below under their
named repositories and reports verified commit signatures. The pinned
`action.yml` files declare these runtimes; this is a source/provenance check,
not execution of the hosted Actions artifact/cache services.

| Action | Selected commit | Declared runtime |
| --- | --- | --- |
| `actions/cache` | `55cc8345863c7cc4c66a329aec7e433d2d1c52a9` | `node24` |
| `actions/cache` | `caa296126883cff596d87d8935842f9db880ef25` | `node24` |
| `actions/checkout` | `3d3c42e5aac5ba805825da76410c181273ba90b1` | `node24` |
| `actions/checkout` | `d23441a48e516b6c34aea4fa41551a30e30af803` | `node24` |
| `actions/download-artifact` | `634f93cb2916e3fdff6788551b99b062d0335ce0` | `node20` |
| `actions/download-artifact` | `9000827ccba6bdab643e8b6fd33ac0654aef8333` | `node24` |
| `actions/setup-go` | `924ae3a1cded613372ab5595356fb5720e22ba16` | `node24` |
| `actions/setup-go` | `b7ad1dad31e06c5925ef5d2fc7ad053ef454303e` | `node24` |
| `actions/setup-node` | `949feb2413d6458794dcd2491c4babbbce0c15c1` | `node24` |
| `actions/upload-artifact` | `cf430e030ddbb5b0abf93d22962f4752f3646cd9` | `node24` |
| `actions/upload-artifact` | `ea165f8d65b6e75b540449e92b4886f43607fa02` | `node20` |
| `docker/build-push-action` | `c3c9e263c25d99ce0380d002d59b67737d91b0dc` | `node24` |
| `docker/login-action` | `8aa5831fb8d48d8f76594c7ddde61ae58019f550` | `node24` |
| `docker/metadata-action` | `dc802804100637a589fabce1cb79ff13a1411302` | `node24` |
| `docker/setup-buildx-action` | `f87e5991a6d7451dcb8d9637bfbc97413f497069` | `node24` |
| `docker/setup-qemu-action` | `99012661954931238ded8c8b007157a8430204e1` | `node24` |
| `googleapis/release-please-action` | `45996ed1f6d02564a971a2fa1b5860e934307cf7` | `node24` |
| `pnpm/action-setup` | `73e46992e05b19abd31f2f3c20e0e7370d762115` | `node24` |

Most selected Actions run Node 24; the retained optimizer artifact Actions run
Node 20. Application Node 26.13.1 is a separate pin. Ubuntu 26.04 runner images,
remote cache behavior and hosted artifact round trips are not proven by the
local host-executor act receipt. In particular, download-artifact's stronger
digest-mismatch failure default is retained; no compatibility override is added.
No hosted qualification is claimed while account billing prevents jobs from
starting, and no publication is performed by this follow-up.

The CP-SAT and HiGHS qualification workflows are restored byte-for-byte to
`60708ce503c039b48062856beb58822c505cb39a`, before #500. Their retained runner and
Action versions must be reviewed with the qualification inputs, not silently
changed underneath source-bound evidence. A Renovate file rule holds these
two workflows until deliberate requalification. The CP-SAT source/evidence
fixture passes again without editing its evidence hashes. HiGHS remains
UNADMITTED under #590; its pre-existing static-base pin mismatch is still an
open qualification failure, not evidence repaired by changing an expected hash.

## Static Command Center basemap

The Command Center includes numeric country-boundary geometry derived from the
primary Natural Earth repository, pinned to
[`9380cca83db5f9aef52d5e762765100745f84b27`](https://github.com/nvkelso/natural-earth-vector/commit/9380cca83db5f9aef52d5e762765100745f84b27).
GitHub reports no verified commit signature for this historical commit. The
GeoJSON source is independently pinned by SHA-256
`6866c877d39cba9c357620878839b336d569f8c662d3cfab4cb1dbe2d39c977f`.
The README at that same commit identifies Natural Earth as public-domain data;
its SHA-256 is
`28b7c031e45c88d7ce20b7782373aaac378f459e9460001dadc8cfd824c3173b`.
The official web terms endpoint returned HTTP 406 during review, so the pinned
primary README is the retained license evidence.

The checked generator accepts only that source checksum and numeric polygon
coordinates within degree bounds. It drops feature properties, rounds the
coordinates to three decimals and removes collapsed geometry. The resulting
local asset has SHA-256
`845579c98eff6a3a8c7a7c0d96e855a5d7fe9964704277717b21fc44cf62deca`;
unit checks bind its bytes, ring/point counts and numeric bounds. No executable
third-party package or remote map/geolocation service is added. These checks
establish the reviewed static bytes and parsing boundary, not a supplier
certification, political-boundary authority, live site location or network path.

## Offline workspace schema pack

The pack vendors data schemas from the primary Zarf v0.82.0 and UDS Core
v1.17.1 tagged repositories. `workspace-schemas/v1/manifest.json` records exact
source URLs, SHA-256 values and the retained upstream root-license hashes (Zarf Apache-2.0; UDS Core AGPL-3.0).
These are reviewed bytes, not signed supplier certification. Neither upstream
schema contains an external schema reference; the gate rejects one even if its
checksum is refreshed. The Conduit-native Plan-input schema is regenerated
from the actual Go types rather than copied from a third-party Plan model.

Diagnostic verification adds Red Hat's MIT-licensed `yaml-language-server`
2.0.0 (published 2026-10-16) and `vscode-jsonrpc` 9.0.3 as test-only dependencies.
The complete npm lock pins registry URLs and integrity for the 21-package
graph; lifecycle scripts are disabled. The npm advisory audit during this
review reported zero known findings, which does not establish absence of
vulnerabilities. The production schema pack contains no npm modules. The real
server is exercised unprivileged in an official Node 26.13.1 bookworm-slim
Linux/amd64 image pinned by manifest digest, with networking disabled, read-only
mounts, no capabilities and no credentials. Image verification is bounded to
that digest; no hosted/editor-image qualification or publication is claimed.
See `docs/operations/workspace-schemas.md` for source-target mismatch handling,
modeline precedence and the editor-image integration boundary.

UDS Core’s root license text is AGPL version 3; per-file schema applicability
has not been separately adjudicated. Retaining that notice does not resolve
project licensing or authorize distribution of an editor image (#546).

## A2UI browser schema validation (2026-11-15)

AJV 8.20.0 is pinned as a console build dependency for standalone validators.
The registry records publication at 2026-04-24T15:22:16.529Z, upstream
`ajv-validator/ajv` source commit
`0fba0b8e649909613cfce0999b149cd08f4a4987`, and MIT licensing. Its tarball
integrity is retained in the frozen pnpm lockfile, alongside the newly selected
fast-deep-equal 3.1.3, fast-uri 3.1.9, json-schema-traverse 1.0.0 and
require-from-string 2.0.2. The install used the existing release-age policy and
ignored lifecycle scripts; the resulting lockfile audit reported zero known
advisories. This is registry/source-coordinate and advisory evidence, not a
publisher attestation or exhaustive code certification; no npm attestation was
reported for the AJV artifact.

The build compiles only retained primary schemas and the reviewed first-party
catalog. Incoming agent messages and catalogs are never passed to the compiler.
The browser imports committed standalone validators and fixed tiny AJV runtime
helpers, not the schema compiler or require-from-string. Generated imports are
allowlisted; an unexpected runtime import fails generation. Preserve AJV and
transitive package license notices when distributing the compiled console.
The project distribution/licensing decision in #546 remains open.

## pnpm 12.20.0 qualification

The [12.20.0 release](https://github.com/pnpm/pnpm/releases/tag/v12.20.0)
cleared the three-day Renovate quarantine. Its signed annotated tag resolves to
`ea74c2cf907bd95d8f69daf3ef778b7f8d92860d`. The downloaded Linux x64 archive
has SHA-256 `568fd50dd174a57e80efacc04be1d6a228bb41fbebe557f2ef63a1c3a721a5f4`.
GitHub's SLSA attestation verified against that digest, source commit, tag and
`pnpm/pnpm/.github/workflows/release.yml`; a modified archive was rejected.
The direct bundle redirect was unavailable, so verification used the inline
bundle returned by GitHub's attestation API, with the same cryptographic checks.
The attested runner is self-hosted, as in the earlier qualification.

Mise's seven platform archive coordinates and checksums match the publisher's
release metadata. Execution and SLSA artifact verification here cover Linux x64;
other platform binaries were not executed. Workflow, package-manager, container
and mise pins move together. A frozen install succeeds with the qualified tool.
Application package versions and snapshots remain unchanged. The new tool
normalizes the Vitest catalog override to its existing effective `5.1.0` lock
specifier and removes the unused catalog mirror. The refreshed web advisory
scan reports zero findings in all severities; this does not establish that the
package manager's entire bundled closure is benign or advisory-free.

## AWS CLI 2.40.2 qualification

The [2.40.2 source tag](https://github.com/aws/aws-cli/tree/2.40.2) resolves to
`058b113d98423d9d3b17105bdbf628495a466b81`. The release cleared the three-day
Renovate review delay. Mise's separate seven-day setting filters fuzzy version
selection; the [pinned mise documentation](https://github.com/jdx/mise/blob/v2026.8.3/docs/security.md)
explicitly states that exact version pins bypass that filter. No age setting or
exception is changed by this update.

The official Linux x86_64 archive has SHA-256
`a9ac0d635e97fab65e234ae1519fa6bd2b02d72120bf965a8ff6eaf7611f1833`.
Its detached GPG signature verifies with fingerprint
`FB5DB77FD5C118B80511ADA8A6310ACC4672475C`, obtained from
[AWS's installation guide](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html).
The key is authenticated through that publisher HTTPS source, not an independent
web-of-trust certification. A modified archive fails signature verification.
The extracted executable reports `aws-cli/2.40.2` with Python 3.14.8 on Linux
x86_64. Verification does not invoke cloud operations or use AWS credentials.

The ARM Linux archive's checksum and AWS GPG signature also verify. The macOS
package's downloaded checksum matches the lockfile; its Apple package signature
is not verified here. Neither platform is executed. The existing musl aliases
point to the same upstream Linux archives; this review does not establish musl
compatibility. Artifact identity and a version check do not establish that the
bundled dependency closure is benign or that every AWS service operation works.

## Claude CLI 2.1.323 qualification

The [2.1.323 release](https://github.com/anthropics/claude-code/releases/tag/v2.1.323)
was published after its tag resolved to
`89eb88e3357be13ae7fcf8966144ad32f44def33` and cleared the three-day Renovate
delay. All seven mise platform checksums match its `SHASUMS256.txt`. The manifest's
detached GPG signature verifies with fingerprint
`31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE`, checked against the key and fingerprint
published in [the publisher's setup guide](https://code.claude.com/docs/en/setup).
This authenticates the manifest through the publisher HTTPS trust source;
independent web-of-trust certification is not claimed. A modified manifest fails
signature verification.

The downloaded Linux x86_64 archive matches SHA-256
`522255678548859b9799b7b098c03a3aa286c9a9ede01957e35f4713ccc43631`.
Its executable reports `2.1.323 (Claude Code)` under an empty home with no inherited
credentials and nonessential traffic/updating disabled. This is a version check,
not a model request or agent execution. Other platform artifacts are covered by
the signed checksum coordinates, not local execution. No source-to-binary
attestation or review of the complete bundled dependency closure is claimed.

## Dockerfile frontend 1.29 qualification

The [upstream dockerfile/1.29.0 release](https://github.com/moby/buildkit/releases/tag/dockerfile%2F1.29.0)
cleared the three-day Renovate delay. Its annotated tag resolves to
`f55949dea4f03bb9d85b73e9afc9a609291289db`; GitHub reports the tag as unsigned.
Docker Hub's `docker/dockerfile:1.29` index bytes match the pinned digest
`sha256:572cebbc2b15e6f84c077742952674889cb63cd5e9b80abdffcbe587b6f19183`.
Its annotations identify version 1.29.0 and the same source revision, and its
platform manifests include Linux amd64 and arm64.

The pulled amd64 frontend, run with networking disabled, a read-only filesystem
and dropped capabilities, reports `dockerfile/1.29.0` and that exact revision.
The four Containerfiles retain immutable digest pins and otherwise unchanged
contents. This establishes image identity, source-coordinate agreement and an
amd64 version check. It does not verify an image signature or build attestation,
execute the arm64 frontend, or establish a complete application image build.

BuildKit v0.34.0 `--call=check` passes on all four tracked Containerfiles with
this frontend, using an isolated tracked-tree context. This checks frontend
parsing and build configuration against the pinned base-image metadata without
executing application build steps; it is not a completed image build.
