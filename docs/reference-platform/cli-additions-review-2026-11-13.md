# November 2026 CLI additions review

Current implementation profile (2026-11-14): 111 pages after #363's discussion
and atomic label commands. Its new immutable snapshot preserves the original
107 rows below and adds the four reviewed Change Request pages. The public live
inventory remains 123. Native output/transport details are documented in
`../architecture/change-request-activity.md`; the review below records the
original November 13 baseline.


Reviewed 2026-11-13 for #336 against the 105-command 2026-08-15 profile.
All 18 new reference pages were read and their Markdown hashes verified against
`cli-command-inventory.json`. This is public documentation evidence, not a
native binary or private protocol compatibility claim. The live inventory stays
at 123 pages. The reviewed profile advances to 107: the original 105 immutable
source rows plus the two implemented Grype pages. Old command sources retain
their August hashes; the snapshot date records this migration, not a recapture
of those 105 pages. The August snapshot remains immutable.

## Classification and ownership

Group pages count as additions even when they do not execute an operation.
The table uses the official command names, without the `apollo-cli` prefix or
Experimental suffix. Every source link and SHA256 identifies the reviewed page.

| Command | Current capability and disposition | Source SHA256 |
| --- | --- | --- |
| [changerequest comment](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_changerequest_comment/) | Hub labels/SetLabels exist; comments and safe CLI mutations: [#363](https://github.com/jonathanagustin/conduit/issues/363). | `4a31116a5b5a44bb17842858b0ed84695e612d528012d71bdf62cc3035f00bbc` |
| [changerequest label add](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_changerequest_label_add/) | Hub labels/SetLabels exist; comments and safe CLI mutations: [#363](https://github.com/jonathanagustin/conduit/issues/363). | `97bda17f213f600a4eb707019f80325e4e112329332544ddf58d7618080fb595` |
| [changerequest label remove](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_changerequest_label_remove/) | Hub labels/SetLabels exist; comments and safe CLI mutations: [#363](https://github.com/jonathanagustin/conduit/issues/363). | `c210bda518bc327cd03d328e22ff9d30e590bf004ef435babd289f86046a4803` |
| [changerequest label](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_changerequest_label/) | Hub labels/SetLabels exist; comments and safe CLI mutations: [#363](https://github.com/jonathanagustin/conduit/issues/363). | `becf7a7c747931c60d790c1bf42b0a5f39655a443c040df10f5ca329a58e9d07` |
| [cve grype scan](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_cve_grype_scan/) | Implemented locally; installed Grype, no Hub data. Added to reviewed profile. | `ae00240c6b5a84b62c09dd9240a28027e1eed4dc8f222165731f905e216858f2` |
| [cve grype](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_cve_grype/) | Implemented locally; installed Grype, no Hub data. Added to reviewed profile. | `e81d2ad990b39d8a661006300992d39286957e5beab6f6e698da5c1cd7228721` |
| [entity maintenancewindowoverride create](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_entity_maintenancewindowoverride_create/) | Change-type vocabulary only; permanent Environment bypass is insufficient: [#364](https://github.com/jonathanagustin/conduit/issues/364). | `68dd90ba43e602faaecc5eae4d4d1d921d23f1b30d9f264068ed6ce000ccb661` |
| [entity maintenancewindowoverride](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_entity_maintenancewindowoverride/) | Change-type vocabulary only; permanent Environment bypass is insufficient: [#364](https://github.com/jonathanagustin/conduit/issues/364). | `9b14ec637879f80dd7bcc0fc5105861c2ad6b1c11c8d3ef9d150bc935da6f54b` |
| [product-release release-channel edit](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_product-release_release-channel_edit/) | Desired-state memberships/add exist; governed edit/removal: [#365](https://github.com/jonathanagustin/conduit/issues/365). | `fd81efc60f5fcd34b29bbd6291fb8f735d8f7aa0bb5f29a4b5bf36bf32e109b2` |
| [product-release release-channel](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_product-release_release-channel/) | Desired-state memberships/add exist; governed edit/removal: [#365](https://github.com/jonathanagustin/conduit/issues/365). | `b9866ed64baf7e1fa724269d55d326cf900d50d7660bd2b18bab6aaca79ab377` |
| [release-channel remove](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_release-channel_remove/) | Desired-state memberships/add exist; governed edit/removal: [#365](https://github.com/jonathanagustin/conduit/issues/365). | `972d3e7f2249ffe6c7497935d7456bf3499835439a5e84af2575c828324a6625` |
| [terminal grant request](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_terminal_grant_request/) | Grant vocabulary only; policy/approval/expiry: [#304](https://github.com/jonathanagustin/conduit/issues/304), dependent on #303 sessions. | `93281c4bee6ef85fa8608eae9845637dc3907651dd2acbfa1e543b7c1cfda03d` |
| [terminal node list](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_terminal_node_list/) | No broker/session implementation; [#303](https://github.com/jonathanagustin/conduit/issues/303), with elevated NODE access via #304. | `0eb26b3eac441e3037f3063686d7e7d1cd533055d2f42e4f76966c68ead12b05` |
| [terminal node](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_terminal_node/) | No broker/session implementation; [#303](https://github.com/jonathanagustin/conduit/issues/303), with elevated NODE access via #304. | `dc735bdb25205baa05a6529813f4266f4e50876c1b3381a714f16c9c49c57e1f` |
| [terminal session attach](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_terminal_session_attach/) | No broker/session implementation; [#303](https://github.com/jonathanagustin/conduit/issues/303), with elevated NODE access via #304. | `7b50bc47c74c19dc4a25d75931a60973dc074803025256bbec3e78749ec51159` |
| [terminal session create](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_terminal_session_create/) | No broker/session implementation; [#303](https://github.com/jonathanagustin/conduit/issues/303), with elevated NODE access via #304. | `6cd367e8d958c02d9b0227757264455596d6733994ab36a3cb3b4b8a11ac69a3` |
| [terminal session list](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_terminal_session_list/) | No broker/session implementation; [#303](https://github.com/jonathanagustin/conduit/issues/303), with elevated NODE access via #304. | `42be337f9941be30948abbcc02a29b5cbec2d28ae2a4a1cb22108caa8fd9c5df` |
| [terminal session](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_terminal_session/) | No broker/session implementation; [#303](https://github.com/jonathanagustin/conduit/issues/303), with elevated NODE access via #304. | `0af9386097d4732a83f46c18adeb1e59ec94b9bdefeb694550f89bf5d00bd477` |

## Local Grype behavior and boundary

`conduit cve grype scan SOURCE [flags] [-- GRYPE_FLAGS]` runs the installed
`grype` executable directly, forwarding the source, stdin, and pass-through
flags. Grype owns image/directory/archive resolution and vulnerability database
access. This wrapper fetches no Apollo or Hub vulnerability data. Grype may
itself contact registries and its database service. Output-control flags are
blocked because normalization requires JSON. Reports are bounded to 32 MiB.

A valid report is printed even when Grype exits nonzero, and the command then
fails. Invalid reports print no report and do not overwrite `--output-file`.
Stdout supports JSON or YAML; `--output-file` always writes JSON through a
private temporary file and atomic rename. Provider stderr is discarded because
it can expose registry credentials. Errors identify the stage or exit status.

NVD CVE identifiers and scores are preferred, then CVSS v3 over v2, falling
back to Grype severity without a recognized score. Only the selected v3 score
is retained. Package, fix, and unknown envelope fields remain. Ignored-match
reasons remain attached while their nested findings are normalized too.

Public documentation does not define the exact JSON extension schema,
tie-breaking between equal candidates, or zero-score severity vocabulary.
Those are registered Conduit choices (`grype.LocalNormalizedReport`):
`conduitNativeNormalization` marks `CONDUIT_NATIVE`, retains original finding
identity, and explicitly disclaims authoritative artifact assessment. Ties use
score, then lexical CVE ID; v3 zero is Negligible, v2 zero is Low. A sanitized
Apollo CLI report would displace these choices. Local output is never ingested
as the Hub's immutable-artifact assessment, and the strict Hub scanner remains
unchanged.

Behavioral tests use an executable fixture to check arguments without shell
interpretation, stdin, output-file privacy/format, policy-exit handling,
malformed reports, blocked output flags, NVD/CVSS selection, severity boundaries,
and provenance. They exercise the wrapper contract; they do not claim a real
image scan, native Apollo binary equivalence, or private report schema parity.

## Remaining product semantics

#363 owns persisted discussion and label additions/removals that preserve
unrelated labels, governance, and approval transitions. #364 owns one request
per unique entity, a shared start/end interval that expires while waiting for
approval, positive duration up to 3600 hours, optional downtime, and partial
submission reporting. #365 owns membership changes rather than channel
definition deletion, editor cancellation before validation, explicit empty-list
removal, merged/deduplicated coordinates, rationale, and dry-run behavior.

#303 additionally owns the documented new debug Pod (not merely exec into a
workload), profile selection, POD/NODE modes, target-node listing, session
create-and-connect/list/attach, and Environment/space scoping. #304 owns named
policy grants, policy-default duration, compliance metadata, dry-run, approval,
and expiry. These requirements were added to their existing issue threads.
The 16 unimplemented additions remain outside the profile until those product
capabilities and behavioral tests land.

