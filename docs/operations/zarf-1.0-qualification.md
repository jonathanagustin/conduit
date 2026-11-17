# Zarf 1.0 qualification candidate

Issue [#652](https://github.com/jonathanagustin/conduit/issues/652) tracks promotion
of the developer, verification and offline-schema targets together. This candidate
is not a completed qualification or permission to deploy or publish.

The v1.0.0 seven-day release hold elapsed on November 16, 2026 at 22:23:47
America/New_York. Upstream v1.0.1 remains held until November 18 at 18:08:13.

## Primary inputs

- [v1.0.0 release and breaking changes](https://github.com/zarf-dev/zarf/releases/tag/v1.0.0).
- Linux amd64 asset SHA256: `c58cf0241796525d9df9a449a5a2218743efa446b540a996f355d71349852857`,
  checked against the release checksums and existing mise lock.
- [Exact upstream schema](https://raw.githubusercontent.com/zarf-dev/zarf/v1.0.0/zarf.schema.json),
  retained byte for byte with SHA256
  `ffb43dc23c17491a48e081c2ea5fe763089cf746b4d2a7beb162dd82b2b9273d`.
  Its Apache-2.0 license bytes are unchanged. There is one pinned schema target;
  no union of old and new schema files is installed.
- [State model](https://github.com/zarf-dev/zarf/blob/v1.0.0/src/pkg/state/state.go)
  and [Secret encoding](https://github.com/zarf-dev/zarf/blob/v1.0.0/src/pkg/cluster/zarf.go).

## Compatibility findings

Zarf 1.x writes gzip-compressed deployed-package JSON. The observer now accepts
legacy plain JSON and gzip JSON with a 16 MiB decoded limit. Corruption,
truncation, excessive expansion and trailing JSON return no deployment record.
Old CLIs cannot read state written by 1.x; reverting a binary pin is not a
qualified state rollback.

Native `DeployConfig` now records deployment variables and values. This partly
displaces the old variable-evidence limitation. Conduit's applied record still
maps the observed package digest/generation to a Conduit Release and parent UDS
Bundle. This candidate does not replace that mapping with desired values or
claim live configuration convergence from the native snapshot.

The current Conduit packages remain v1alpha1, which 1.0 supports with deprecation
warnings. Migrating their variables/actions to v1beta1 is separate work. The new
init defaults, compressed state, removed storageclass/distro state and changed
init inputs still require cluster acceptance. No Zarf Go SDK is imported here;
removed SDK bridges do not establish CLI compatibility by themselves.

## Evidence and remaining gates

Native v1.0.0 archive generation and admission pass, including rejection of stale
output, symlinks, invalid content, wrong identity/platform and ambiguity. State
race tests and vet pass, and restoring the original state parser makes the
compressed-state regression fail. Six drift-guard cases pass across developer,
lock, workflow and schema targets; schema hashes and contained references pass.

Pinned native Zarf passes 1,401 action-only operational-selection cases, seven
chart-parity profiles with the same 91 listed baseline differences, and package
and generated-scaffold lint. Real Zarf/UDS trust and rendered-bundle creation
checks also pass, as do the schema Go tests. The real language server reports
correct valid/invalid YAML/YML diagnostics under UID 65534 in a network-none
namespace with byte-identical fixtures.

Fresh container allocation exhausted this worker's disk. Package checks reused
the existing isolated Ruby container with an in-memory executable whose checksum
matched the primary release. Editor diagnostics reused that container with the
pinned host Node binary; this is not the fresh read-only-image wrapper or a full
CI receipt. Complete exact-head CI remains a promotion gate. Existing validation
containers and original demo files remain intact. No cluster install, upgrade,
disconnected operation, publication or signing is claimed by these checks.
