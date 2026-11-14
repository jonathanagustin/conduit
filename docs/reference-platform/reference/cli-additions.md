# November 2026 CLI additions

Sources and exact reviewed hashes are indexed in `review-ledger.tsv`; the
[complete 18-command classification](../cli-additions-review-2026-11-13.md)
records each command, capability, and product issue.

Two local Grype commands and four Change Request discussion/label command
pages (#363) are implemented and behaviorally tested. The other 12 additions
remain outside the reviewed profile: temporary entity maintenance overrides (#364), channel
membership editor/removal (#365), and terminal sessions/grants (#303/#304).
An existing label setter, permanent Environment bypass, or channel-definition
operation does not establish those new command semantics. Terminal create
starts and connects to a new debug Pod; it is not just workload exec.

The reviewed profile is 111 source pages: the original 105 August rows, two
November Grype rows and four Change Request activity rows. The live inventory independently remains 123. Neither
count proves native binary output equivalence or an unpublished private protocol.
No refusal-only commands were introduced for these additions.

## Change Request activity implementation

`changerequest comment` takes one existing Conduit request RID and required
`-c/--comment`. It creates durable discussion with server-recorded identity and
time, without changing reviews, approval rationale or Plan state. The native
`comments` reader follows bounded pages and survives restart. Annotation uses
the existing author/reviewer mutation authority; reading uses request visibility.
These record, route, access and output choices are Conduit-native; the pinned
CLI metadata does not establish a private discussion protocol.

`changerequest label add|remove` takes one request and required `--label-name`.
Normal new names resolve active definitions; the existing `Do not merge` name
is built in and attached legacy names remain removable. The server transforms
the current list, retaining concurrent unrelated edits, audit and approval/Plan
behavior. `--dry-run` returns an explicit preview without repository or audit
writes. Existing replace-list callers share the governed transformation.

`TestChangeActivityCLIUsesHubAuthorityAndSurvivesRestart`,
`TestLabelAddRemovePreserveConcurrentEditsAndPreviewHasNoWrites` and the real
PostgreSQL discussion/restore gates detect drift. The reviewed snapshot
`../snapshots/cli-command-sources-2026-11-14-363.tsv` preserves the previous
107 rows and adds exactly these four command pages; it does not recapture the
other source pages or claim native binary output equivalence. See
[`change-request-activity.md`](../../architecture/change-request-activity.md)
for native bounds and persistence semantics.
