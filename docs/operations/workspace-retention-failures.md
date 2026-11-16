# Git-backed workspace retention failures

The Git store expires only stopped workspaces with an elapsed preservation
deadline and no recorded active runtime. It checks cancellation and requires an
expiry intent receipt before confined removal. Deletion and completion-receipt
errors stop processing; successfully persisted records form an ordered prefix.
Catalog errors roll back the in-memory record so a later call can retry.

A removal can complete before its completion receipt or catalog update fails.
The preserved flag then remains pending; retry performs idempotent removal and
finishes the metadata transition. The intent receipt remains evidence of the
attempt. This is not an atomic filesystem, audit and catalog transaction or a
power-loss durability guarantee.

Idle preservation distinguishes a failed branch push whose retention metadata
was saved from a hard catalog failure. Only the former permits compute cleanup.
Missing origin is a failed push, not evidence that a branch was preserved
remotely. Fixed automation commit subjects use Conventional Commits.

Workspace IDs must be single bounded alphanumeric, underscore or hyphen
components. Removal uses an `os.Root` handle, so a substituted parent symlink
cannot redirect deletion outside the configured store. This boundary does not
sandbox editor or agent processes, coordinate multiple store processes, or
discover orphaned workloads. Catalog loading rejects invalid and duplicate IDs
rather than silently adopting them. Operators must repair an invalid catalog
before reopening it.

Tests exercise cancellation, active workspaces, exact deadline boundaries,
failed deletion, both audit failures, catalog failure, retry after partial
completion, and parent-symlink escape using temporary directories. No live
workspace volumes are removed during verification.
