# Environment labels in the public CLI reference

## Reviewed public contract

The five Experimental `apollo-cli label` pages were reviewed on 2026-11-17:
[parent](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_label/),
[delete](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_label_delete/),
[get](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_label_get/),
[list](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_label_list/) and
[set](https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_label_set/).
Exact source hashes are in the appended [`review-ledger.tsv`](review-ledger.tsv)
rows and current public-documentation manifest.

All five pages now include `environment` among their supported resource types,
using the full Environment ID, including its suffix. The setter adds an
Environment example. Product Release coordinates remain supported. No other
resource kind is established by this list.

The change is bounded by the prior hashes: removing the added Environment
resource line from each current source, and removing the setter's Environment
example and reverting its example-label pluralization, reproduces all five
previous manifest hashes exactly. This accounts for the eight additional words
on four pages and eighteen on the setter. Command synopses, flag sets and the
four headings per page are unchanged from the previous current inventory.

The unchanged operations identify a label by `--label-id`. Get fails when the
assignment is absent, list reads all assignments, set assigns or overwrites its
`--label-value`, and delete removes the assignment. Set and delete retain their
non-mutating preview option. This is a public documentation contract; it does
not establish private APIs or native executable output equivalence.

## Conduit boundary

Conduit's existing `label get|list|set|delete environment <id>` already resolves
the exact Environment ID. An abbreviated name, a different suffix or different
case fails before a Hub connection is opened. No runtime command behavior was
added for this evidence refresh.

The native workflow still requires `--file`. Get and list read that desired-state
document, rather than fetching live labels from Apollo. Set and delete submit
the resulting next-generation desired state through the ordinary Conduit Plan
proposal boundary; they do not edit the file, approve the Plan or mutate a live
Apollo resource. Dry-run prints the proposed state without connecting to the
Hub. The existing Entity label target, file format, proposal transport and output
records remain Conduit extensions.

The mutable 123-page CLI inventory follows the refreshed manifest. The immutable
116-page reviewed CLI profile and its source snapshots retain their earlier
coordinates; this review does not silently relabel that profile as a new native
CLI release.

## Verification

`TestEnvironmentLabelCLIUsesCompleteIDAndKeepsTheNativeProposalBoundary` exercises
all four real Cobra commands with a suffixed Environment ID, the two mutation
previews, preservation of unrelated state and an unchanged input file. Its
application port records the submitted proposal and detects approval bypass.
`TestEnvironmentLabelCLIRejectsAbbreviatedAndDifferentIDsBeforeConnecting`
checks all four commands against abbreviated IDs, a different suffix and case.
These are CLI/application-boundary tests, not live Apollo or Hub authorization
conformance tests.

Run `go test -race ./internal/conduitcli`, the offline
`scripts/verify-conduit-cli-contract.mjs`, and the explicitly networked
`scripts/verify-documentation-manifest.mjs` to verify the corresponding bounds.
