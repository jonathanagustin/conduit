# Zarf 1.0.1 patch qualification

PR [#1177](https://github.com/jonathanagustin/conduit/pull/1177) coordinates
the developer lock, CI downloads, runtime executable gate, scaffold admission
and structural-validator schema name. Updating only the developer lock would
leave those consumers on different targets.

The upstream release is [v1.0.1](https://github.com/zarf-dev/zarf/releases/tag/v1.0.1),
published November 11, 2026 at 23:08:13 UTC. Its seven-day toolchain hold
elapsed November 18 at 23:08:13 UTC. The release tag resolves to
[`e62a47c31c2a10854bd3f9d64f31345dffa26c68`](https://github.com/zarf-dev/zarf/commit/e62a47c31c2a10854bd3f9d64f31345dffa26c68).
Linux amd64 executable SHA256 is
`73f64e4c2d9ca2eba5c5067e5507e00b9ec553ff66acece497d2062bf5815a6b`.
The local download matches both the release asset digest and upstream
checksums, and reports exactly `v1.0.1`. Other platform entries retain their
upstream checksums; this worker does not establish their execution behavior.

The schema at that immutable source revision is byte-identical to the
previous vendored schema: SHA256
`ffb43dc23c17491a48e081c2ea5fe763089cf746b4d2a7beb162dd82b2b9273d`.
The license bytes also match. Only active target metadata and validator
selection change; historical receipts and the original 1.0.0 qualification
remain unchanged. `scaffold-input-zarf-v1.0.0.json` retains the old fixture
bytes. New scaffolds require the active 1.0.1 pin; an explicit 1.0.0 input is
refused rather than silently reinterpreted. Existing receipts are not renewed
or relabeled by this change.

Qualification is scoped to native rendering, lint, action-only selection and
archive/conformance tests. It does not establish cluster deployment, init
upgrade or rollback, disconnected operation, production signing, or complete
binary vulnerability/provenance admission. Existing 1.0.0 cluster evidence
continues to describe the binary actually used. The separately pinned UDS
CLI and its embedded Zarf are unchanged. Publication and deployment remain
manual and require their own authority.

## Bounded local results

- Target consistency and eight drift tests pass, including stale executable
  checksum and one stale workflow download among otherwise correct jobs.
- Runtime template compilation, native lint, executable-selection guards and
  all 1,401 action-only operational selection/refusal cases pass. Five focused Go race-test packages and their vet checks pass.
- Nine structural-validator tests pass with the exact pinned dependencies.
  An initial ambient run refused `rpds-py` 2026.6.3; selecting the declared
  2026.9.1 runtime fixes the harness without relaxing the dependency check.
- Native UDS bundle creation passes both production renderer paths, wrong
  digest and missing-component refusals. Executor conformance records all ten
  required run/PASS pairs, including correct/wrong-key and unsigned crypto,
  child configuration isolation and scalar values. Runtime bundle assembly
  retains the authenticated image-free beta package and refuses unsigned,
  wrong-key and alpha inputs. No cluster is used.

Hosted final-head gates remain required before promotion. The original 1.0.0
source-review assertions in the executor remain explicitly dated evidence;
this patch does not relabel their cluster-state observations.
