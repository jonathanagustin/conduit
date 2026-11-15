# Recorded packaging pipeline views

The Packaging area's Pipeline tab uses the existing `s07-pipeline` screen.
Timeline uses `s08-staged-run`. The run list, both screens and their navigation
share a page-local run selection. The shared area header and default inspector
context use that same visible run, and the header uses the run list status
classification rather than assuming success. There is no second packaging workspace and
no new command endpoint. A selected run must still occur in the currently
visible index; a missing selection never silently switches its detail to
another run. History is looked up by that exact run ID, including when a
history request failed. Opaque IDs are held in prototype-free dictionaries.

The graph is a read-only rendering of the Hub's `StepEvent` journal, from
`GET /v1/packaging-runs/{id}/events`. The loader follows opaque pagination
cursors on the same route, with at most 16 pages and 256 events. Repeated,
invalid or oversized cursors, malformed pages and read failures retain an
explicitly incomplete result. A later-page failure preserves the already read
prefix and marks it incomplete. No request writes or advances a stage.

A stage chunk groups adjacent events with the same step name. Returning to an
earlier step after another step creates another chunk. The view preserves
server order, flags sequence gaps/repeats and never sorts by wall clock or
synthesizes unreached stages. Its graph is recorded order, not a dependency DAG.
The selected node has an inspector, a recorded-output preview and separately
labeled stage/run references. Staged detail shows ordered chunks, inputs,
artifact/evidence availability and recorded output. Reference IDs do not prove
Plan approval, artifact verification or deployment success.

Status comes from recorded outcomes. Required review, blocked state and an
explicitly unconfigured worker cannot become success merely because an earlier
result was PASS. Unknown outcomes have no success styling. There are no fixed
sample runs, durations, completion percentages, hardware signatures or sample
log lines on these screens. Stage output is the journal's redacted `reason`
with its sequence/time/outcome; it is not a claim of raw subprocess logs or
complete execution history beyond the worker's bounded journal.

The current status contract supplies digests and references, not artifact
body downloads, an SBOM or signature verification receipts. Those sections
state what is absent; they never join an unrelated global bundle preview to a
run or manufacture a download URL from a digest. Pipeline edits still require
their governed change-request path. Recorded progress does not grant a Plan or
approval operation. Existing review feedback remains on its existing route.

This is a Conduit UI extension over the native packaging status/history
contract, not a private reference-platform compatibility claim. Unit and
Chromium tests use owned records and HTTP fixtures, checking selected-run
isolation, revisited stages, sequence gaps, cursor continuation/failure/loops,
escaping, real area dispatch and cross-screen selection. They establish no live
packaging build, signature verification, cluster run or publication authority.
