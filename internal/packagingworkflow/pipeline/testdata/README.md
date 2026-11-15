# Recorded workflow replay fixtures

`packaging-v2-history.json` is a synthetic localhost history captured from
`PackagingWorkflow` at source commit
`866ccad705f84dfb3b757c74ac0a3e7a00643074`, before the version-3 terminal-event
change. The existing real restart/handoff test supplied the fixture application,
restarted workers and Temporal, and retrieved the completed execution history.
The capture used the unchanged version-2 workflow through a Go source overlay;
it did not edit the candidate implementation or hand-write Temporal events.

The tools were Temporal CLI 1.10.0, embedded server 1.32.2, and embedded UI 2.56.1,
as pinned by `scripts/fetch-temporal-cli.sh` and checked by the recovery test.
The history contains only the repository's synthetic fixture identities and
bounded activity receipts. It is not a tenant capture. Existing recovery checks
refuse fixture file bodies and credential-shaped material in workflow history.

SHA-256: `40c8ffdb0109b5a733becefce781c148119692f34ae2e97ca1b3f78b3c074f4a`.

`TestVersionTwoRecordedHistoryReplaysWithCurrentWorkflow` checks the actual
version-2 marker and replays this history using the current workflow. The
separate mocked version-2 cancellation test checks its query-visible journal
behavior; Temporal command replay alone cannot prove a query projection stayed
unchanged.


`packaging-v3-history.json` is a synthetic localhost history captured from
`TestDevServerRestartRecoversWaitAndHandoff` at source commit
`6a14bc8fab9f52dee09954991d532cf365852d6c`, before the version-4 rotating-journal
change. The workflow implementation was unchanged; a Go source overlay added
only JSON capture to the existing real restart/handoff test. The same pinned
Temporal CLI/server/UI versions and redaction checks above apply. No Temporal
events or version markers were hand-written. This is repository fixture data,
not a tenant observation.

SHA-256: `7eb3d3e8305190abc40e687a132601a8c9e4314b4033191b54be630fe94e5986`.

`TestVersionThreeRecordedHistoryReplaysWithCurrentWorkflow` checks the real
version-3 marker and replays with the current workflow. The separate full-journal
v3/v4 cancellation test constrains query-visible stop/rotate behavior; command
replay alone cannot establish that a query projection stayed unchanged. The
real recovery test now also starts a v4 run with a full carried journal, cancels
it, restarts worker and server, compares projected source records and replays
its recorded history.
