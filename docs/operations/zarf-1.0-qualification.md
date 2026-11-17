# Zarf 1.0 qualification candidate

Issue [#652](https://github.com/jonathanagustin/conduit/issues/652) tracks the
coordinated developer, CI, schema and package migration. This candidate has
scoped local evidence; complete exact-head CI remains a promotion gate. It
establishes no live cluster, disconnected-operation or production signing claim.

## Primary inputs

- [Zarf v1.0.0 release and breaking changes](https://github.com/zarf-dev/zarf/releases/tag/v1.0.0).
- Linux amd64 asset SHA256:
  `c58cf0241796525d9df9a449a5a2218743efa446b540a996f355d71349852857`,
  checked against the primary release checksums and mise lock.
- [Exact upstream schema](https://raw.githubusercontent.com/zarf-dev/zarf/v1.0.0/zarf.schema.json),
  retained byte for byte with SHA256
  `ffb43dc23c17491a48e081c2ea5fe763089cf746b4d2a7beb162dd82b2b9273d`.
  Its Apache-2.0 license bytes are unchanged. There is one editor schema target.
- [Native state model](https://github.com/zarf-dev/zarf/blob/v1.0.0/src/pkg/state/state.go)
  and [Secret encoding](https://github.com/zarf-dev/zarf/blob/v1.0.0/src/pkg/cluster/zarf.go).
- [UDS v0.41.0 legacy bundle deployment](https://github.com/defenseunicorns/uds-cli/blob/v0.41.0/pkg/legacy/bundle/deploy.go#L212)
  and [native values forwarding](https://github.com/defenseunicorns/uds-cli/blob/v0.41.0/internal/zarf/deploy.go#L383).

The v1.0.0 seven-day hold elapsed November 16, 2026 at 22:23:47
America/New_York. Upstream v1.0.1 remains held until November 18 at 18:08:13.

## Compatibility and implementation

Zarf 1.x stores gzip-compressed deployed-package JSON. The observer accepts
legacy plain JSON and gzip JSON, with a 16 MiB decoded limit. Corruption,
truncation, excessive expansion and trailing JSON yield no deployment record.
Old CLIs cannot read 1.x state; reverting a binary is not a qualified rollback.
Native DeployConfig records variables and values, partly displacing the old
variable-evidence limitation. Conduit's applied record still binds observations
to a Conduit Release and parent UDS bundle; it is not a live convergence proof.

Four owned generators and the runtime now use v1beta1. Runtime source is
`zarf.tpl.yaml`, compiled in a fresh private directory. Sealed package metadata
binds images; schema-backed deploy values and setValues-derived settings replace
legacy variables/constants. One optional `uds` owner uses typed UDS_METRICS
selection. Plain confirmed installs select no UDS component. Deprecated choice
members and groups are removed, without adopting the proposed image-origin
flavors in ADR 0017. Native health waits remain present.

The Spoke authenticates standalone package definitions before cluster I/O.
Alpha packages retain variable flags; beta packages receive private absolute
values files preserving reviewed JSON scalar types. Cleanup covers success,
preflight refusal and deployment failure. Lifecycle commands authenticate each
release's identity/version/image binding and stage registry archives once, so
inspection, deployment and readback use the same local artifact.

UDS 0.41.0's legacy YAML path forwards SetVariables and chart overrides, not
native package Values. NextMode supports HCL values_files and enables its Zarf
SDK values feature. The owned runtime bundle now uses that HCL route. Its build
authenticates a copied beta archive against the exact release-workflow identity,
or an explicitly supplied public key. It refuses unsigned and alpha inputs.
Assembly leaves the outer bundle unsigned for a separate explicit signing step.
The old owned YAML bundle/config files are retired; generated legacy YAML
consumers retain their frozen schema reference and tests.

`UDS_NATIVE_BUNDLES=true` opts a Spoke into the NextMode verify/deploy/remove
commands. Legacy remains default without verification fallback. The native path
retains the admitted public key and architecture. Global scalar HCL inputs
preserve types and escape interpolation as literal data; legacy scoped keys
refuse before authentication. Native values files refuse non-scalar inputs.

## Scoped evidence and limits

- Generator race/vet checks, 9 structural-validator cases and 8 target-drift
  cases pass. Native generated-fixture lint rejects deprecation warnings.
- Runtime checks pass 1,401 native action-only cases and 7 Helm parity profiles
  with the same 91 reviewed differences; all 51 parity mutations are rejected.
  Metrics off/on each render exactly one UDS Package owner.
- Structural security and fresh native lint checks pass, including empty/failed
  tool and warning controls. Wrong model binding, mismatched sealed image,
  removed image guard and broadened plaintext admission each fail their gate.
- Lifecycle gates pass 14 definition-reader and 14 stubbed command cases.
  A removed pre-deploy image guard is rejected. This is not a cluster rehearsal.
- The race-enabled executor gate requires 10 exact run/pass receipts, including
  actual CLI type/literal forwarding and unsigned native-artifact refusal.
  Startup race and affected schema/adapter race/vet checks pass. Removing HCL
  escaping makes the actual CLI regression fail; restored source passes.
- The owned bundle gate signs an image-free beta fixture with an ephemeral test
  key, assembles it through the real build entry point and refuses unsigned
  input. Its committed config example and values file forward exactly through
  a resource-free action fixture; nested input refuses before any action.
- A separate local fixture verifies with its matching ephemeral key and refuses
  a wrong key and unsigned artifact. This is not production signing/publication.
  The Release Please engine passes 17 tests with the HCL version marker.

The earlier aggregate CI run failed from disk exhaustion. About 1.4 GiB was
recovered from generated dependencies in clean older checkouts, preserving
source and demo files. Scoped Ruby checks reused the existing isolated container;
its memory-backed executable matched the pinned checksum. Editor diagnostics
also passed in that container's network-none namespace with byte-identical
fixtures and pinned Node; this was not the fresh read-only-image wrapper.
Complete exact-head CI must still pass. Init defaults, changed init inputs,
cluster upgrade and disconnected operation retain separate acceptance needs.
