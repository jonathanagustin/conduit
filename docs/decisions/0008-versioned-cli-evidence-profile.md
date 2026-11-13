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
