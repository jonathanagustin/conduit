# Cursor browser alignment review — 2026-08-09

This review tested the operator console against the public evidence and bounded
contracts in `CONTROL-PLANE-UI-PLAN.md`, `upstream-screen-evidence.tsv`,
`API-GAPS-DERIVED.md`, and `FIDELITY-ALIGNMENT-2026-08-07.md`. It does not claim
private Apollo compatibility.

## Method and receipt

Two heterogeneous Cursor evaluator runs completed with an approved,
repository-scoped Playwright MCP. Grok and Kimi each inspected all 11 canonical
captures and the live console at desktop and mobile viewports. Expected 404s
from the unstubbed Hub API were treated as test inputs, not product defects.

| Model family | Complete browser access | Input | Output | Cache read | Provider duration |
| --- | ---: | ---: | ---: | ---: | ---: |
| Grok | yes | 120,695 | 11,440 | 3,878,784 | 433,991 ms |
| Kimi | yes | 125,474 | 32,079 | 11,445,828 | 976,641 ms |

Monetary cost was not returned by Cursor and remains unknown, never zero. An
Opus attempt obtained browser access but did not complete a bounded report; it
was interrupted after an unbounded filesystem search. Its partial output is not
counted as evaluation evidence.

The captures lagged the working source for two already-fixed details (grammar
and approval-button order). Findings were therefore checked against current
source or the live DOM before disposition.

## Independently corroborated priorities

1. **False clear on unreadable Overview sources.** Both complete runs observed
   six `Clear` panels when all Hub reads failed. This contradicted the source
   comment and exact-state invariant. The fix in this change gives every panel
   an explicit `Unknown / Could not be read` state and adds a mixed-success
   browser test proving readable zeroes remain `Clear`. The later native
   `GET /v1/overview` projection preserves that contract with six independently
   `CURRENT`/`UNAVAILABLE` panels, exact counts, at most five narrow items, one
   shared Plan snapshot, and one browser read instead of four.
2. **Plan ResourceDiagnostics are not rendered.** Both runs found that the Hub
   models resource-level failure evidence but the Plan transport/view drops it.
   This was closed in the following slice: the typed projection now renders
   every bounded/redacted diagnostic under its exact execution task, open by
   default for failures, with semantic resource coordinates and a mobile-safe
   definition-list layout. The browser gate proves two different resources,
   reasons, namespace, messages, later-task visibility, and collapsed raw
   output; deterministic capture fixture `06-plan-detail` records the result.
3. **Product detail lacks its promotion pipeline.** Both runs found the graph
   only on the fleet-wide LOCAL extension despite Product detail being the
   stronger evidence-aligned home. This is now closed: Product detail requests
   the existing `GET /v1/promotions?product=` projection, renders the shared
   semantic graph and constraint detail, calls an unevaluated pipeline unknown,
   and leaves Release evidence usable if the promotion read fails.
4. **Overview structure is too dashboard-like.** Both runs questioned six
   permanently visible panels. This remains a hypothesis until the corrected
   unknown state is re-captured and tested with realistic operator workloads.
5. **Keyboard and mobile contracts are incomplete.** Kimi browser-proved the
   missing skip link and lost popover focus; both noted that populated wide
   tables lack a measured mobile alternative. The skip link is tested, and the
   Environment populated-table slice is now closed: Entities, scoped Plans,
   and Commands retain semantic desktop tables and switch below 768 px to
   equivalent definition lists from the same records. A 390 px browser gate
   proves each table is hidden, each card is visible, long coordinates and
   rationale create no page overflow, and axe reports no violations. Plans now
   also expose native links that complete the Environment → Plan detail path.
   The first gate exposed and closed a preexisting skipped-heading defect in
   the Environment summary. Populated tables outside Environment remain open.
   Applications focus restoration is
   also closed: keyboard activation no longer double-toggles the native button,
   focus enters the first menu item, and Escape returns it to the trigger. The
   browser gate failed against the prior implementation before passing the fix.
   Review of the exact `@blueprintjs/core@6.18.0` source commit
   `a60d4c92257612808fbfac81cfeee4fcba91a8b4` replaced a proposed legacy
   `Popover` event-handler workaround with the supported `PopoverNext`, whose
   target implementation explicitly avoids native button double activation and
   whose focus-return default is tested upstream.
6. **Applications were hardcoded and their menu items were not native links.**
   This is now closed by the authenticated, bounded `GET /v1/session`
   projection. The console renders only known enabled application IDs, fails
   visibly without a session projection, and exposes native `href` targets.
   Server endpoints remain the authorization authority; the projection never
   grants access.

## Worktree proposal protocol

Future evaluator rounds should pair critique with executable proposals. Each
model receives the same immutable base commit, evidence bundle, acceptance
contract, browser capability, and resource budget, then creates one isolated
git worktree and branch. It may edit only that worktree and must return:

- its patch commit and changed-file manifest;
- tests that fail on the base and pass on the proposal;
- browser captures for the affected states and viewports;
- exact commands, duration, token usage, cost status, and residual unknowns.

The primary worktree remains verifier-owned. It compares patches by independent
acceptance gates, invariant preservation, measured operator outcome, complexity,
and lifecycle cost. Evaluators cannot merge, push, approve, or mutate delivery
state. Worktrees are retained until disposition is recorded, then removed with
`git worktree remove`; no proposal relies on `/tmp` for durability.

This protocol is a trial, not yet the default. Its exit criterion is one
three-arm round in which every proposal is reproducible from its commit, at
least two materially distinct patches are compared, and the durable disposition
records whether the selected patch outperformed the primary implementation.
