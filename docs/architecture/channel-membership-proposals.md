# Governed channel membership proposals

Issue #365 implements three previously hash-pinned CLI command pages. Public
syntax establishes the editor, coordinate inputs, rationale and dry-run flags;
it does not establish a private API or editor/result schema.

`product-release release-channel edit GROUP:NAME:VERSION` opens `$EDITOR`
(falling back to `vi`) with a private YAML file containing `releaseChannels`
and, unless supplied by flag, an editable blank `rationale`. Saving an explicit
empty list removes every membership. Unchanged bytes, an empty/null document,
or nonzero editor exit cancel without submitting or validating edited content.
Malformed saved YAML, unknown fields and multiple documents are errors. A bare
YAML channel list is also accepted; it leaves rationale blank unless a flag was
supplied. A supplied rationale flag cannot be overridden by a second editor
field. Normal Governance whitespace normalization applies to persisted rationale.

`release-channel remove CHANNEL` merges and deduplicates repeatable
`--maven-coordinate` values and a YAML-list `-f/--maven-coordinates-file`.
Every coordinate must exist. Success reports the submitted coordinates and
Plan; it does not delete the channel definition. The command is Experimental,
matching the reviewed command page. Files and edited documents are bounded to
1 MiB. Both commands require structured JSON/YAML output before mutation.

## Native Git-owned projection

Both commands require Conduit-native `--state-file` and `--next-generation`.
The file is the current Git-owned desired state; the second flag names the new
reviewed Git generation. The CLI does not change files in Git or publish a
commit. It previews or submits a normal governed Plan carrying the modified
memberships. Existing desired-state validation still applies: for example,
removing the last release from a channel referenced by an Entity can leave an
invalid desired state and is rejected. The CLI does not silently change that
Entity's selection or artifact coordinates to make the document valid.

Dry-run is offline and submits nothing. It previews desired state, coordinates
and review context; it does not claim server authorization or freshness.

Submission uses the existing `POST /v1/plans` with explicit native media type
`application/vnd.conduit.plan-proposal.v1+json`. The strict envelope contains
`desiredState` and `review` (title, rationale, original state digest and
`CONDUIT_NATIVE` tier). Ordinary JSON Plan proposals retain their existing shape.
The bounded `planreview/domain` context owns review metadata validation; the
existing Governance service retains persisted Change Request authority.

The server reuses original-carrier operator authentication and Environment
proposal authorization. Inside a serialized repository boundary it compares the
source digest with the latest stored desired state. Only channel memberships
and the explicit new generation may differ; channel definitions, artifact
coordinates and every other field remain unchanged. Changed channel names must
exist, new membership cannot target an archived channel, and the effective
principal needs the existing channel contribution authority (or an explicit
root proposal grant). Referenced-resource policy inputs use that effective
principal; the original author remains recorded by existing Governance.

Every changed Release produces a native
`CONDUIT_NATIVE_UPDATE_RELEASE_CHANNEL_MEMBERSHIP` review change containing the
coordinate, before/after memberships and baseline digest. Membership-only
proposals therefore cannot bypass approval by having no Entity execution diff.
Existing policies must explicitly decide the new type; all-no-op fails closed.
The title/rationale reach the persisted Change Request rather than being echoed
only in CLI output. Existing review and Plan execution remain in force.

Memory/PostgreSQL transactions serialize validation and writes. The one-process
file adapter holds its visibility mutex across the baseline check and durable
writes, but does not promise crash-atomic multi-file transactions. A stale
source fails with conflict, including a repeated submission after successful
acceptance; re-read the latest state before a new edit. There is no automatic
merge of concurrent membership proposals.

## Evidence and verification

The new immutable `cli-command-sources-2026-11-14-365.tsv` snapshot preserves
the previous 111 rows and adds only the three reviewed command pages. Earlier
snapshots remain unchanged. The reviewed profile is 114 pages; the independently
captured live inventory remains 123. The three invention register entries name
the evidence that would displace the native projection and transport.

`TestMembershipEditorCancellationAndEmptyListAreDistinct` and
`TestMembershipRemovalMergesCoordinatesAndPreservesReviewAndArtifacts` drive the
real CLI and Hub, including persisted rationale and restart.
`TestMembershipProposalsAreGovernedBoundedAndSerialized` exercises memory,
file and PostgreSQL, rejects unauthorized/artifact/definition changes and
stale concurrent proposals, and requires the native membership approval.
