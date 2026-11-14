# Change Request discussion and label activity

The reviewed CLI contract establishes `changerequest comment REQUEST_RID -c
TEXT` and `changerequest label add|remove REQUEST_RID --label-name NAME`, with
`--dry-run` on label operations. It does not establish a private API, discussion
record, or mutation result. Those choices are registered Conduit extensions and
carry `CONDUIT_NATIVE`.

| Public CLI page | Pinned content SHA-256 |
| --- | --- |
| [comment](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_changerequest_comment/) | `4a31116a5b5a44bb17842858b0ed84695e612d528012d71bdf62cc3035f00bbc` |
| [label](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_changerequest_label/) | `becf7a7c747931c60d790c1bf42b0a5f39655a443c040df10f5ca329a58e9d07` |
| [label add](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_changerequest_label_add/) | `97bda17f213f600a4eb707019f80325e4e112329332544ddf58d7618080fb595` |
| [label remove](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_changerequest_label_remove/) | `c210bda518bc327cd03d328e22ff9d30e590bf004ef435babd289f86046a4803` |

The reviewed profile advances from 107 to 111 pages only after the behavioral
Hub/CLI tests. The new source snapshot preserves the earlier 107 rows and adds
these four rows. The live inventory remains 123 pages. The additional native
`changerequest comments REQUEST_RID` reader does not count as a recovered public
command. Conduit uses the request IDs already exposed as its request RIDs; it
does not translate unpublished private identifiers.

## Discussion

`POST /v1/change-requests/{requestID}/comments` accepts one strict JSON document
with `text`. The server supplies a request-scoped random comment ID, effective
author, original credential actor and UTC time. Text is nonempty, at most 16 KiB,
and retains its whitespace and line breaks. Actor/author identifiers are bounded
to 512 bytes. Clients cannot set creation identity, time or provenance tier.

Annotation uses the same request author/reviewer authority as existing label
mutations, with the original carrier preserved. Reading uses the existing
request-read policy. These are native permission choices, not recovered unpublished private
rules. A closed request remains discussable by an authorized author or
reviewer. A discussion comment neither approves a request nor replaces approval
rationale, reviews, execution history or Plan state.

`GET /v1/change-requests/{requestID}/comments` returns chronological pages of at
most eight comments. The cursor binds request, time and ID and grants no access.
The page size keeps even maximally JSON-escaped records below the operator
client's response bound. Pagination is an ordered live view, not a frozen
snapshot. The native CLI reader follows pages and produces no partial success
output if a later page fails.

Discussion has its own private append-only file records or PostgreSQL
`change-discussion` component. Older delivery snapshot writers cannot erase
it. Stored identity/index mismatches, malformed records and failed reads return
errors rather than an empty thread. File mode uses exclusive installation and
is not advertised as a multi-file transaction store. PostgreSQL has an
independent migration and the physical restore drill verifies both discussion
and its request, with ledger checks before constructors.

## Labels and Plans

`POST /v1/change-requests/{requestID}/label-mutations` accepts `labelName`,
`operation` (`ADD` or `REMOVE`) and optional `dryRun`. Newly attached normal names
resolve active label-definition IDs. This is name-only Change Request tagging;
it does not infer an Environment/Entity label value from a definition's accepted
values. The existing `Do not merge` name is built in. Attached legacy tags remain
removable, and tombstoned definitions do not strand removals.

The existing request author/reviewer rule, approval requirement state,
do-not-merge behavior and audit remain in Governance. Both the old replace-list
route and the new single-label route share that behavior. Add/remove transforms
the serialized current list and preserves unrelated concurrent edits.
Authorization is retried when the request revision changes before the write.
Successful writes include the matching Plan projection. Ordinary review
synchronization reads the current request in the same repository lock or
PostgreSQL transaction as its Plan projection, so an older caller snapshot
cannot restore an approval after a newer hold. Proposal still projects its
explicitly evaluated verdict as before.

File mode serializes and persists both records before acknowledgement. It does
not claim crash-atomic multi-file writes: an I/O error is an error and a retry
can repair projection. PostgreSQL commits the paired update transactionally.
Concurrency guarantees apply to the corrected runtime; legacy-writer retention
of discussion does not establish old-runtime review behavior.

A native mutation result has `APPLIED` or `PREVIEW` mode. Preview describes the
prospective labels/status/approval transition and performs no request, Plan or
audit writes. Actual submission checks current state again. Mutation commands
reject unsupported output formats before submitting anything. Discussion and
label activity require a configured target Hub; previews are server-validated,
unlike the separate offline download-asset preparation dry run.
