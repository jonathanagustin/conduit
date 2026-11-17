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

The candidate migrates owned package generators and the runtime source to
v1beta1. The runtime uses native package templates for sealed image bindings,
a schema and values file for deploy inputs, and `setValues` for derived settings.
One optional `uds` component owns the Package CR; its typed `UDS_METRICS` setting
selects the monitor. A plain confirmed install selects no UDS component. The
retired `uds-metrics` name is refused rather than creating a second owner.

The task and publication build callers now compile the package template into
fresh temporary directories and pass the generated definition explicitly to
`package create`. Publication, signing and deployment were not executed.
The Spoke now selects legacy variable flags or native typed values from an
authenticated package definition before cluster I/O. Its private values file is
removed on success, preflight refusal and deployment failure. The lifecycle
lane now authenticates each release definition and selects alpha variable flags
or beta values files. Registry sources are pulled once into a fresh local
archive, used for inspection, deployment and image readback. Its definition
reader and stubbed command tests each pass 14 positive/refusal cases; this is
not a live cluster install/upgrade/removal receipt. The pinned UDS bundle route
still needs deploy-value integration.

UDS CLI v0.41.0 imports Zarf v0.89.0. Its
[legacy YAML deployment path](https://github.com/defenseunicorns/uds-cli/blob/v0.41.0/pkg/legacy/bundle/deploy.go#L212)
sets `DeployOptions.SetVariables` and `ValuesOverridesMap`, not native package
`Values`. Chart overrides cannot supply the runtime manifest values. The
[new bundle path](https://github.com/defenseunicorns/uds-cli/blob/v0.41.0/internal/zarf/deploy.go#L383)
does parse package `values_files` and forwards native `Values`, enabling the SDK
values feature at initialization. A successful legacy `uds create` does not
establish deployment compatibility. Migrating the owned YAML bundle to that
new contract, or an explicit refusal of unsupported inputs, is still required;
no deployment compatibility is claimed from source inspection alone.

The pinned release binary exposes the new command tree only with
`--features NextMode`: its commands are `bundle create`, `bundle verify` and
`bundle deploy`, rather than the legacy top-level commands. This is a whole
consumer-contract change, including HCL configuration and signature flags;
setting a values flag on the old command is insufficient.

A local image-free beta-package fixture assembled successfully using
`uds --features NextMode bundle create ... --unsigned`. Native inspect reports
its package `valuesFiles`, and the resulting OCI archive contains the exact
input values bytes (SHA256
`73d2e4b9a9b5a0f0c9fd4f9dffbf79efbba15aa80dacda0d976d223f387cae8f`).
This intentionally unsigned fixture exercises assembly only. Invoking native
`bundle verify` without a trust policy refuses with `invalid bundle verification
policy`; inspect explicitly reports that signature verification was not done.
No trust check was bypassed on a production artifact. No bundle deployment,
cluster acceptance, publication or signing was performed for this probe.
The new init defaults, compressed state, removed storageclass/distro state and
changed init inputs still require cluster acceptance.

## Evidence and remaining gates

Native v1.0.0 archive generation and admission pass, including rejection of stale
output, symlinks, invalid content, wrong identity/platform and ambiguity. State
race tests and vet pass, and restoring the original state parser makes the
compressed-state regression fail. Eight drift-guard cases pass across developer,
lock, workflow and schema targets; schema hashes and contained references pass.

The earlier pin-only candidate passed 1,401 native action-only operational-selection cases, seven
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

## Runtime migration checks

The migrated runtime matches Helm for all seven baseline and operational profiles
with the same 91 reviewed differences. Its native manifest renderer agrees with
the bounded parity helper, including quoted and Unicode values; schema refusals
are checked with the actual pinned CLI. The migrated source also passes all 1,401 native action-only selection and
validation cases and rejects all 51 chart-parity mutations. Native rendering
checks metrics off and on: each emits exactly one Package owner, and only the
enabled setting includes its monitor. These are separate from the earlier
pin-only result.

`scripts/render-zarf-runtime.sh` compiles into a fresh private output directory,
refuses mutable or unsafe image references, existing directories and symlinks,
and removes an incomplete output after a failed compilation. Its native positive
case and those refusal cases pass. Generated `zarf.gen.yaml` is ignored; the
reviewed source is `zarf.tpl.yaml`. These checks do not establish a successful
full package gate or complete CI for this work-in-progress migration.

The package-contract gate now reads beta image objects and optional components
directly and derives deploy declarations from the actual values schema/defaults.
Distinct symbolic bindings retain the structural security assertions; they are
test sentinels, not legacy declarations. The native parity gate independently
checks real templating. Lint compiles into a fresh directory, checks both owned
package targets and rejects deprecation warnings. Empty/failed tool and warning
controls pass. Wrong model bindings, mismatched sealed images, a removed image
guard and broadened plaintext admission each make the structural gate fail;
the restored baseline passes. This validates structure and native lint, not the
legacy UDS bundle's deploy-value forwarding.

## Native bundle adapter checkpoint

The Spoke can explicitly select UDS NextMode with `UDS_NATIVE_BUNDLES=true`;
legacy remains the default, without automatic verification fallback. The native
commands retain the configured public key and architecture. Reviewed global
scalar inputs use a private HCL configuration, with string interpolation escaped
as literal data. Legacy package-scoped keys are refused in this mode.

An actual action-only native bundle fixture carries strings, booleans, integers
and literal `${...}`/`%{...}` sequences through HCL configuration and values files
without changing their types or bytes. It contains no cluster resources. Native
unsigned-artifact refusal is part of that CLI regression. Separately, the pinned
CLI accepted a local fixture signed with an ephemeral test key, rejected the
wrong key and rejected an unsigned fixture. These fixture signatures are not
production signing or release publication. The owned runtime bundle source and
operator route still need migration before this PR can be promoted.
