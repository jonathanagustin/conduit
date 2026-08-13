# Private Apollo interoperability evidence

The parent Apollo evidence rules apply. This folder records only sanitized,
authorized evidence needed to evaluate private Hub/Agent compatibility.

- Never infer endpoints from role names, Maven coordinates, UI labels, or
  Agent-to-Foundry Install Manager types.
- Keep raw traffic, binaries, credentials, tenant URLs, certificates, and demo
  media under `/tmp`; commit only reviewed schemas, hashes, and derived notes.
- A protocol claim requires request/response fixtures for authentication,
  leasing/polling, start, heartbeat, cancellation, finish/report, retry, and
  version negotiation, plus executable conformance tests.
- A native CLI byte claim requires the sibling `cli-fixtures` exact comparison
  gate. Semantic reconstruction is not a substitute.
