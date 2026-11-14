# November 2026 CLI additions

Sources and exact reviewed hashes are indexed in `review-ledger.tsv`; the
[complete 18-command classification](../cli-additions-review-2026-11-13.md)
records each command, capability, and product issue.

Two local Grype commands and four Change Request discussion/label command
pages (#363) and three membership editor/removal pages (#365), plus two temporary Entity
maintenance pages (#364), are implemented and behaviorally tested. The other
7 additions remain outside the reviewed profile: terminal sessions/grants
(#303/#304).
An existing label setter, permanent Environment bypass, or channel-definition
operation does not establish those new command semantics. Terminal create
starts and connects to a new debug Pod; it is not just workload exec.

The reviewed profile is 116 source pages: the original 105 August rows, two
November Grype rows and four Change Request activity rows, three membership rows and two temporary maintenance rows. The live inventory independently remains 123. Neither
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

## Release-channel membership implementation

The membership editor/removal commands now create real governed Plan proposals,
retain rationale in the Change Request and preserve channel definitions and
immutable artifact coordinates. Saved empty lists remove memberships; empty
documents, unchanged editor content and nonzero editor exits cancel before
submission. Removal merges repeatable coordinates and YAML-list file inputs.
Offline dry-run performs no submission.

The YAML schema, strict review envelope, native membership Change Type, source
digest comparison and required Git-oriented state/generation flags are Conduit
extensions, not a recovered private API. Existing desired-state validity can
reject an edit that leaves an Entity selecting an empty channel.
See [channel-membership-proposals.md](../../architecture/channel-membership-proposals.md)
for authority, concurrency, persistence bounds and behavioral tests. The new
114-row immutable snapshot preserves the previous 111 rows; no upstream page
was recaptured during this implementation.

## Temporary Entity maintenance implementation

The Experimental parent/create commands (also `entity mwo create`) now submit
one normally governed request per unique Entity across Environments, with a
common server submission interval, positive duration at most 3600h, default
no-downtime, retained reason/ticket and partial-failure results. Approval waiting
consumes the interval. Dry-run creates no request.

The native batch transport, structured receipts and required reason are explicit
Conduit extensions. Separate maintenance-aware Agent admission projects current
approved windows into copies without persisting them into Plans. Ordinary/older
Agent leases retain the base schedule. New Agents require the upgraded Hub and
recheck window expiry immediately before forward mutation, including after Helm
rendering. See [temporary-maintenance.md](temporary-maintenance.md).

Both current public Markdown sources were re-fetched on 2026-11-14 and matched
their existing pinned hashes. The new 116-row snapshot preserves all 114 prior
coordinates; it does not recapture or replace the old snapshots.
