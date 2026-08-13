---
description: Create the next Markdown Architectural Decision Record (MADR) for a durable architecture choice.
---

Create a MADR for the architectural decision described in `$ARGUMENTS`.

1. Read the Zenith ledgers and existing `docs/architecture/[0-9][0-9][0-9][0-9]-*.md` records.
2. Confirm this is a durable architecture decision. If it is a working-method experiment, use a PDR;
   if it is work to perform, use the backlog; if it is an uncertain fact, use the assumptions ledger.
3. Select the next four-digit number without renumbering existing records and create
   `docs/architecture/NNNN-short-kebab-title.md` from `${CLAUDE_PLUGIN_ROOT}/templates/MADR.md`.
4. Record context, measurable decision drivers, genuinely viable options, the outcome and accepted
   trade-offs. Link immutable evidence and any PDR/experiment/value assessment that informed it.
5. Start at `proposed`. Change to `accepted` only when the named confirmation gates pass and approval
   exists. Use `rejected`, `deprecated`, or `superseded` without rewriting history; link replacements
   in both directions.
6. Run the relevant executable gates plus Zenith conformance. Report the record and remaining unknowns.

MADR records explain why architecture changed. They never replace tests, exact state, authorization,
rollback, capability closure, or the append-only measurement ledger.
