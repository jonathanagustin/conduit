# November label CLI documentation delta

Reviewed 2026-11-17 against the published Markdown page data. The five URLs,
current content hashes and review disposition are in `review-ledger.tsv`; the
complete report-only crawl found 284 pages, no additions/removals, five changed
pages and no contentless responses. No page bodies or media are committed.

## Derived public contract

The `label` command family remains experimental. Its four operations identify a
resource by type and user-facing identifier; the documented resource kinds are
Environment and Product Release. Get/delete identify a label id, list reads the
resource's labels, and set writes a label id/value. The command-specific flags
and the two positional arguments agree with the repository's pinned November 14
metadata.

All five current pages now list the inherited `--sso-callback-port` setting: it
selects the loopback listener port for the SSO return, with zero selecting an
available ephemeral port. Those five entries in the pinned November 14 CLI
metadata do not list that flag; other entries already do. This is an inherited
authentication-configuration delta, not a new label operation. No images are
referenced by these five Markdown bodies, so there is no image-only contract
delta to inspect for them.

The older complete Markdown bodies were not retained. The small word-count
changes therefore cannot establish a byte-for-byte prose diff; the bounded
comparison here is the current reviewed semantics and its parsed command/flag
metadata against the pinned metadata, not a claim that every wording change is
irrelevant.

## Implementation consequence and bound

This inventory refresh does not update the immutable November 14 CLI snapshot,
change native label behavior, or claim native support for the upstream login
flow. Interactive native human sign-in and refresh are already tracked in
[#818](https://github.com/jonathanagustin/conduit/issues/818); that work must
state its loopback-listener contract explicitly rather than infer one from
inherited flag names. The generated public native CLI reference is built from
Conduit's own command tree and must not gain flags merely because this source
lists them.

The weekly documentation workflow can now resume after a real reviewed refresh.
It checks drift without `--write`, requires the network crawl to complete, and
does not publish or alter a committed evidence date on its own.
