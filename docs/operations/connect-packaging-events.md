# Packaging-run event history over Connect

`POST /conduit.packaging.v1.PackagingRunService/ListPackagingRunEvents` mirrors
`GET /v1/packaging-runs/{runID}/events`. Both use the same native page reader,
parent-run visibility check, credential redaction and seek pagination. The
contract is Conduit-native; it is not a subscription, an outbox, a resume
protocol or evidence of a private upstream API.

The request names `runId` and accepts `view`, optional `limit` and opaque
`cursor`. Full or omitted view defaults to 20 and permits at most 20; summary
defaults to 100 and permits at most 200. Explicit zero/negative limits, invalid
views and malformed or mismatched cursors retain native validation failures.
No actor, authority or approval can be selected in the message.

Authentication precedes decoding and store access. The original credential
carrier remains authoritative under delegation. An absent or invisible parent
run is not found, and its event journal is not read. Missing or failing history
stores are not successful empty histories. An entirely unconfigured reader
retains the existing authenticated `configured: false` page with no invented
parent run ID.

The response preserves `CONDUIT_NATIVE`, configuration, packaging/Plan/approval
ownership, run ID, counts and cursor metadata. Events retain sequence, time,
step, outcome, redacted reason and source/artifact/proposal/approval/Plan/target
references. These references grant no authority. Sequence determines ordering;
a step absent from the journal has not passed. Counts describe the retained
native journal rather than an invented complete workflow history.

ProtoJSON encodes sequence/count int64 values as decimal strings. Occurrence
times use `google.protobuf.Timestamp`, preserving instants and normalizing
zones. A stored time outside that type's supported range produces a sanitized
unavailable error rather than a fabricated time. Unknown step/outcome strings
remain open native vocabulary. Compare typed values, not raw REST JSON bytes. Byte-truncated Unicode reasons
retain REST JSON's decoded replacement characters rather than failing protobuf
encoding. The same normalization applies to the existing run-detail projection.

Native validation (REST 422) maps to Connect invalid-argument; a hidden/missing
parent remains not-found. Unexpected store errors become sanitized unavailable
errors, with no backend diagnostics in the response. Permission refusals are
never retried through another transport.

The existing proto gate generates and checks both clients. Read parity tests
cover forward/backward cursors, redaction, typed timestamps, unconfigured and
failing stores, authentication before body/store reads and hidden-parent
refusal. Composition tests compare real delegated carrier decisions to REST;
a generated Go client also reads the new procedure over HTTP. The console and
CLI keep their existing callers until their own explicit migration changes.
General live updates remain SSE under [ADR 0016](../decisions/0016-console-live-events.md).

Refs #391, #392, #393 and the [cutover plan](connect-cutover.md).

## Operator console reader

The active workbench refresh uses the generated `ListPackagingRunEvents` client
for each run, following opaque cursors in recorded order. Requests retain
same-origin credentials, the refresh abort signal and a ten-second per-page
timeout. Auth and validation failures map to `HubError` and never replay through
REST. The existing bounded view retains a successfully read prefix on a later
failure and labels it incomplete (16 pages / 256 events maximum).

ProtoJSON event sequences are checked before conversion to the renderer's safe
integer representation. An out-of-range sequence fails the page rather than
rounding recorded evidence. Totals remain `bigint`; nanosecond timestamps are
formatted through the generated Timestamp contract. A partial count cannot be
mistaken for complete history. Worker-unconfigured empty reads remain distinct
at the client boundary. Initial, manual and live packaging index reads also use
their generated client. Commands, Plan status and approvals retain their own
transport and governance contracts.

## Legacy React data helper

`console/data/hub.ts` uses the same generated list and event clients, retaining
its existing return DTOs and first-page-only behavior (20 rows/events). It does
not follow returned cursors or claim a complete journal. Both helpers accept an
optional abort signal; same-origin identity and native `HubError` categories
are retained, without REST replay. Event sequences outside the renderer's safe
integer range are refused instead of being rounded or replaced with zero, and
nanosecond timestamps remain exact. This helper migration is separate from the
active workbench cutover above; it adds no second conversation or approval path.
