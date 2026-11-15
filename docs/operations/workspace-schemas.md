# Offline workspace schemas

`workspace-schemas/v1` is the versioned schema pack consumed by the future
workspace editor image (#636). Copy this entire directory into
`/opt/conduit/workspace-schemas/v1` read-only and merge `settings.json` into the
editor's administrator-owned YAML settings. Do not just put the file in an
arbitrary directory: its associations use that exact installed path. Image
construction and enforcing editor settings are tracked separately; this change
does not claim a deployed editor or an installed VS Code extension.

The associations cover `*.plan.yaml` / `*.plan.yml`, `zarf.yaml` / `zarf.yml`,
and `uds-package.yaml` / `uds-package.yml` at any depth. Other filenames must be
renamed or given an explicit local association. A Plan input is the Hub's
`DesiredState` document (`EnvironmentDeployment`), not a server-owned PlanRecord.
The native schema is generated from the Go types. It checks structural types,
unknown properties, the document discriminator and essential environment field
presence. Go's non-omitempty output fields are not all mandatory input fields.
Hub semantic checks, immutable artifact checks, authorization, approvals and
execution admission remain authoritative. An editor diagnostic is no grant.

## Sources and limits

The manifest records schema SHA-256 hashes and the exact source versions.
Zarf targets **0.82.0**, the version downloaded and linted by both CI paths.
`mise.toml` currently selects 1.0.0 for developers; that is a known separate
qualification mismatch, not a reason to label a 1.0.0 schema as compatible with
0.82.0. Change the schema target alongside the verified runtime target when
qualifying that upgrade. UDS Package targets the **1.17.1** Core default in
`scripts/uds-up.sh`. Upstream schemas are retained byte-for-byte, together with
Zarf’s Apache-2.0 and UDS Core’s AGPL-3.0 root license files. Their source
and license hashes are in
`manifest.json`. The native schema uses `platform.delivery/v1alpha1` and its
committed generator is checked for reproducibility on every verification.

Every schema reference is a local fragment. The `$schema` draft identifier
identifies the JSON Schema dialect; it is not a remotely loaded schema
reference. SchemaStore discovery is disabled and each association is a file URI.
No runtime download is needed to validate these documents.

The YAML language server gives document `$schema` fields and
`# yaml-language-server: $schema=...` comments precedence over associations.
Remove remote directives from imported documents, or replace them with the
appropriate `file:///opt/conduit/workspace-schemas/v1/...` URI. The repository's
Zarf definition no longer contains its remote directive. Arbitrary user
modelines, user settings or extensions are not controlled by this data pack;
the workspace's default-deny egress boundary (#616) must remain enforced. Do not
turn network access on to make a schema diagnostic disappear.

## Verification

Run `./scripts/verify-workspace-schemas.sh` with the repository's pinned Go,
Node, npm and Docker available. Dependency installation is a preparation step
and requires a connected package cache or registry; it is not editor runtime
behavior. `npm ci --ignore-scripts` uses the committed integrity-checked lock.
The pack itself has no executable dependency on these test packages.

The gate checks source targets, schema/license checksums, fragment-only
references, exact file associations and reproducible native generation. Negative
controls exercise changed bytes, removed associations, enabled SchemaStore and
an injected remote reference. Then the pinned official Node 26.13.1
bookworm-slim Linux/amd64 container runs the real YAML language server 2.0.0 over
stdio, as an unprivileged user, read-only, without capabilities or credentials,
with Docker `--network none`. The test also checks that only loopback exists.
It supplies the shipped settings through normal LSP configuration requests;
the server uses its own schema loader. Each invalid document must produce an
error tied to its installed schema URI; load failures do not count. Valid
fixtures, including the repository's foundation Plan example, must be free of
diagnostics. The image digest is pinned in the gate. Both hosted and local CI
invoke this same complete gate; it does not publish an image.

## Updating the pack

1. Select the Zarf and UDS Core versions actually targeted by the package and
   verification lanes. Qualify runtime upgrades separately; update both CI
   paths together. Do not point the editor at a moving upstream URL.
2. Download the exact tagged upstream `zarf.schema.json` and
   `schemas/package-v1alpha1.schema.json` from the manifest's primary sources.
   Retain upstream bytes and the license at each tag, reviewing changes and
   any new external references. Bundle references locally before adoption;
   this version deliberately rejects non-fragment references.
3. For native type changes, run
   `go run ./tools/workspace-schemas/generate workspace-schemas/v1/plan.schema.json`.
   Review the structural diff and any new input-presence requirements against
   actual Hub validation. Do not expose server-owned lease/approval state.
4. Update manifest versions, source coordinates and SHA-256 values, fixtures,
   settings and legal notices as needed. Increase the pack directory/version
   when the installed document contract or association changes incompatibly;
   keep the image path and settings coordinated.
5. Run the full schema gate, review dependency/advisory changes, and run complete
   local CI on the exact committed head. A checksum edit alone is not evidence
   of compatibility. Include the resulting receipt in the PR. The editor-image
   owner must copy and verify these bytes before claiming an offline editor.

UDS Core’s root license text is AGPL version 3; per-file schema applicability
has not been separately adjudicated. Retaining that notice does not resolve
project licensing or authorize distribution of an editor image (#546).
