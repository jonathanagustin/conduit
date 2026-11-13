# Historical metadata correction, 2026-11-13

The repository owner authorized Conventional Commit subjects and clean
authorship/session metadata throughout active history, explicitly including
published release tags. The correction covered five branch tips and six
release tags in one guarded atomic push.

Every one of the 541 commits in the captured graph was checked for
identical file-tree identity. Parent edges were preserved under the
[old-to-new SHA mapping](../evidence/git-history-sha-map-2026-11-13.tsv).
Commit dates were retained; metadata corrections changed 539
commit IDs, including descendants of corrected commits. Human and dependency-bot
authors were retained; automated attribution identities and session/signature
footers were normalized to repository-owned metadata. Old commit signatures
were removed rather than represented as signatures over changed metadata.

## Release coordinates

| Tag | Previous commit | Corrected commit |
| --- | --- | --- |
| `v0.17.0` | `06bab8c041db9965e1fbddd90a35839bbd9eca17` | `73a049f6ddc82bb42e20d996aff76351269e66cf` |
| `v0.17.1` | `7aa7cee8dd6f1bddf229474ef8b3d362060a5586` | `fa48823e9e79f6a2ebdd2df3e507d1ab6765a87a` |
| `v0.17.2` | `6d3943a3da8277b463a68d730a31ee4828811909` | `bae4495d72dc907ce184e7b7da3921128450d268` |
| `v0.17.3` | `57d166e06820a3b0adb01d556565556827df2150` | `18f13be636b2b757abcbfc15b24ce0184b9dcbbf` |
| `v0.17.4` | `7a0584bf723f142b6af2781cf41c221e1bc6bc13` | `3b0bcbd4eb0daad36add8fb939600cb7a330c5a8` |
| `v0.17.5` | `44e1e8845fe01c2a461061f6557a128ebf716af6` | `92e0c6f6258b68f7bebf5da04978ac54ec337844` |

No version or artifact digest changed, and no artifact was republished.
Existing signed provenance and historical upgrade receipts still refer to
their original source SHAs. They were not reissued or relabeled as proof about
the corrected commit IDs. Use the mapping and identical tree IDs to establish
source-content equivalence; metadata equivalence is not a new signature.

Release Please's explicit bootstrap and its policy check now identify the
corrected `v0.17.5` commit. The published version remains 0.17.5 and the 0.18.0
release proposal remains review-only. Normal release recovery continues to
forbid moving an existing tag; this owner-authorized correction is exceptional.

## Verification and collaborator recovery

Remote tips were compared against captured expected SHAs immediately before
the push, and each ref used an explicit force-with-lease in one atomic push.
All resulting remote tips and their original tree IDs were checked again.
No source patch was applied by that push. CI statuses were not copied onto
rewritten commit IDs; subsequent proposed heads require fresh Trunk results.

Fetch corrected branches and tags with explicit force when replacing old local
tag refs. Preserve uncommitted work before updating a checkout. Rebase pending
changes onto the corrected base; do not merge an obsolete main ancestry back
into the repository. Original objects and local recovery refs were retained
in the working repository for recovery, outside the remote branch/tag set.
