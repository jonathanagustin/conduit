# November 2026 CLI additions

Sources and exact reviewed hashes are indexed in `review-ledger.tsv`; the
[complete 18-command classification](../cli-additions-review-2026-11-13.md)
records each command, capability, and product issue.

Two local Grype commands are implemented and behaviorally tested. The other
16 additions remain outside the reviewed profile: Change Request discussions
and labels (#363), temporary entity maintenance overrides (#364), channel
membership editor/removal (#365), and terminal sessions/grants (#303/#304).
An existing label setter, permanent Environment bypass, or channel-definition
operation does not establish those new command semantics. Terminal create
starts and connects to a new debug Pod; it is not just workload exec.

The reviewed profile is 107 source pages: the original 105 August rows plus
two November Grype rows. The live inventory independently remains 123. Neither
count proves native binary output equivalence or a private Apollo protocol.
No refusal-only commands were introduced for these additions.
