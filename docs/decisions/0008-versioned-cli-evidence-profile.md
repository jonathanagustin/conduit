# Version CLI compatibility separately from the live documentation inventory

Status: Accepted

## Context

The November 2026 merge recovery re-crawled the public Apollo documentation to
clear the 45-day freshness gate. The live CLI inventory grew from 105 to 123
commands. Making that inventory the implementation contract immediately caused
the command-tree test to require 18 new commands, including terminal access
whose implementation is still tracked by separate product issues.

## Decision

Preserve the reviewed 2026-08-15 implementation profile and pin its 105 source
rows in an immutable snapshot. Keep the current 123-command evidence inventory
separate. The CLI evidence gate verifies each against its own source manifest,
rejects independent hash or coverage drift, and reports additions outside the
reviewed profile. The Go command-tree test continues to require every command
in the reviewed profile. Refreshing source evidence does not expand that claim.

The capture script defaults to refreshing the current evidence inventory.
Changing the implemented profile requires an explicit, reviewed migration of
the snapshot, contract, command tree, and tests.

## Consequences

Documentation freshness remains enforced. New commands remain visible and
tracked without introducing refusal stubs merely to satisfy a tree-shape test.
The tradeoff is a second small evidence artifact and a versioned source table.
Neither inventory proves byte-equivalence with a native Apollo executable.

## 2026-11-13 migration

The [18-command review](../reference-platform/cli-additions-review-2026-11-13.md) classifies
every addition and links product work. The reviewed profile advances to 107
pages by adding the implemented local Grype group and scan operation. The new
`cli-command-sources-2026-11-13.tsv` snapshot retains the original 105 rows
unchanged and adds only those two November source rows. Its date identifies the
review migration; it does not imply that the old sources were recaptured.
The live inventory remains 123. The other 16 additions stay outside the profile
until their scoped implementations and behavioral tests are reviewed.

## 2026-11-14 Change Request activity migration

Issue #363 adds the four reviewed Change Request comment/label command pages
after behavioral tests for durable discussion, native Hub/CLI authorization,
atomic concurrent label edits, do-not-merge approval/Plan transitions, and
non-mutating previews. The reviewed profile is now 111 pages; the live inventory
remains 123, with 12 pages outside this implementation profile.

`cli-command-sources-2026-11-14-363.tsv` preserves the previous 107 rows and adds
only those four hash-pinned rows. The earlier snapshot is unchanged. Native
comment records, the additional discussion-list command, transport routes and
preview output are registered Conduit extensions; public command syntax does
not establish private APIs or those unspecified result shapes.
