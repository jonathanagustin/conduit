# AG-UI wire contract

This package validates the public AG-UI **1.0** input/event shapes and the Hub's
explicit start/content/end producer ordering. `upstream/manifest.json` pins the
primary `ag-ui-protocol/ag-ui` November 6 release revision, exact canonical
schema bytes and MIT license bytes. No SDK implementation is copied, no new
runtime dependency is added, and schema compilation rejects external loaders.

The JSON Schema is the wire-shape authority. In this pinned revision,
`ResumeEntry.status` is `resolved` or `cancelled`; the same revision's prose
uses different descriptive terms. The tests use the canonical schema values.
`RUN_FINISHED.outcome` is an object, including its discriminator, not a string.
`RUN_ERROR` does not acquire invented thread/run fields; its enclosing run and
audit context carry that association.

The Hub producer profile starts each accepted run with `RUN_STARTED` and emits
one terminal event. It uses HTTP failures before starting and does not emit the
public protocol's optional late failure after a terminal. Text and tool calls
use explicit boundaries rather than chunk shorthand. A broken exchange is
incomplete; the public HTTP/SSE binding does not use Last-Event-ID resumption.
Recovery must request a new run with a new ID and a fresh snapshot.

Shape validation is not authorization, tool admission, argument-schema
validation, audit persistence, state-patch application or a mounted endpoint.
Those obligations belong to the governed Hub/application adapters and the
console. A valid public tool event alone cannot authorize an operation.
