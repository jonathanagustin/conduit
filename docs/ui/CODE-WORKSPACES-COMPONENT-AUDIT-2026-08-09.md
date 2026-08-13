# Foundry Code Workspaces component audit

Status: private V0 visual validation evidence, 2026-08-09.

At the repository owner's direction, an authorized read-only session inspected
Branches, Tags, Pull Requests, Checks, and one Pull Request overview in a
Foundry Code Workspaces repository. No branch, tag, build, review, comment,
merge, or repository mutation was invoked. Tenant origin, resource identifiers,
repository contents, raw DOM, and screenshots are intentionally absent from
git; ephemeral captures remain under `/tmp` and may disappear on restart. This
sanitized note is UI evidence, not Apollo protocol or private DOM compatibility.

## Observed component matrix

All views share a dark global sidebar, repository breadcrumb/object header,
horizontal Code/Branches/Checks/Pull requests/Tags/Settings navigation with an
underlined active tab, repository status/actions, and a centered content canvas.

| View | Index/detail composition |
| --- | --- |
| Branches | Page heading and primary action; separate Default and Active sections rendered as semantic tables; branch identity, check result/time, Pull Request state, links, and a separated destructive control. |
| Tags | Search, creation-order sort, and primary action; dense expandable cards combining tag, outcome, abbreviated commit, subject, time, commit link, and disclosure to a restricted build-result panel. |
| Pull Requests | Search, Open/Closed state tabs, head/base/author filters, and cards combining title, base←head identity, author, and creation time. |
| Checks | Branch selector and chronological status cards with result, timestamp, measured duration, branch and abbreviated commit linkage; a real bounded “Error loading checks” state preserved branch context and disclosed details. |
| Pull Request overview | Back affordance, object header/state, summary and warning banner; Overview/Files changed/Pipeline review/Security tabs; review/check rows; a sticky capability-sensitive decision bar. No decision control was used. |

Status presentation generally combines icon, text, and color. This redundancy
is useful. Compact controls and disclosures require our existing keyboard,
label, and minimum touch-target gates rather than visual imitation.

## Responsive and semantic boundary

At 390×844 the observed application retained an approximately 230px sidebar
and clipped the desktop canvas, hiding controls and values. This is evidence of
a failure, not a target. Platform retains its narrow shell and complete mobile
definition-list records. Branches used semantic tables; other indexes used
cards. Captures had one visible heading but no `main` landmark; one Tags state
lacked navigation semantics. Platform retains semantic headings, landmarks,
native tables where tabular comparison matters, and axe checks.

## Decisions

- Reuse the information architecture: object header → sibling domain tabs →
  URL-owned filter strip → status-rich narrow records → tabbed detail.
- Give every paginated index a bounded error state that preserves visible query
  context. Retry only idempotent reads and preserve the same query.
- For Plan and Change Request activity, show status text/icon, timestamp and
  duration, immutable identity links, and disclosure to diagnostics.
- The Pull Request overview corroborates a Change Request detail composition:
  constraint banner, overview/evidence/security tabs, checklist rows, and a
  sticky capability-gated decision bar. Platform still owns revision conflict,
  approval, cancellation, and audit semantics; merge behavior is not evidence.
- Raw tenant selectors are not test contracts. POMs use roles/names plus
  Platform-owned domain test IDs keyed by immutable coordinates.

## Explicit non-findings

No loading skeleton, empty collection, pagination control, bulk behavior, row
menu, log disclosure, retry action, build trigger, diff, comment flow, reviewer
dialog, keyboard traversal, or screen-reader announcement was established.

## Ephemeral capture receipt

The following SHA-256 values identify authorized raw captures without storing
or locating them: Branches desktop/mobile `bea6ed9d…`/`92ad789f…`; Tags original
desktop/mobile `decf282e…`/`a01a368a…`; Pulls desktop/mobile
`aab17eb9…`/`0487c92d…`; Checks original/error/mobile
`9ece1216…`/`d8a3c55a…`/`2e5dd949…`; Pull Request overview original/mobile
`715ed0fb…`/`5fda600f…`. Original desktop Tags, Checks, and detail captures were
partly obscured by a modal; clean desktop recaptures produced transient
launch/error states, so content conclusions use only the admissible states
described above.
