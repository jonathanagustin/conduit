# Platform operating ledgers

This subtree adapts Zenith Protocol to Platform's Hub/Spoke reconstruction and
release workflow. Repository and domain-specific instructions remain in force.

- Read the root operating model, capability map, backlog, assumptions, and
  current process trials before starting a substantial slice. Domain facts
  remain in their canonical architecture, Apollo evidence, or contract note;
  these ledgers link rather than repeat them.
- `SOLVED` requires an automated gate. `OPEN` or `BLOCKED` rows name the exact
  file, command, API, or error that makes the next attempt cheaper.
- Every `PH-#` intake row has an ACCEPT, REJECT, NOTE, or QUEUED verdict.
- Process changes start as PROPOSED, may run as a measured TRIAL, and become
  ADOPTED only after their exit criterion is met. Metrics are append-only; add
  a superseding row instead of editing an earlier measurement.
- Run `node scripts/verify-zenith-protocol.mjs` before committing ledger or
  release changes.
