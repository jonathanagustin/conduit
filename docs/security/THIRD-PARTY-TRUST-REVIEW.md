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
| GitHub Actions | All 13 distinct remote Action repositories in workflows use full 40-character SHAs. GitHub resolves each exact commit under its named repository and reports a verified commit signature for each. Existing supply-chain policy checks pass. | Publisher identity/signature and immutable coordinates do not establish benign behavior, review nested dependencies, or prove hosted runtime compatibility. Remaining Action majors belong to #500. |
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
