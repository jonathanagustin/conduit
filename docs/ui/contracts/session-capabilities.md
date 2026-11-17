# Session capability contract

`GET /v1/session` is the console's authority for identity and application
navigation. The response distinguishes the authenticated actor from the
effective principal used for authorization and reports whether delegation is
active.

The projection contains:

- `actor` and `effective`, each with username, teams, truncation, and
  service-account fields;
- `delegated`;
- application IDs with an `enabled` flag;
- operations granted at `platform/root`;
- optional operation lists for requested `resourceRid` values.

The server accepts at most 32 resource coordinates per request, removes
duplicates, sorts them, and rejects a coordinate that is not bounded and
canonical. Team output is capped at 64 entries and sets `teamsTruncated` when
more exist.

The console shows only applications returned as enabled. It must not replace a
failed session request with a hardcoded capability set; it reports that
capabilities are unavailable and leaves application navigation incomplete.
Route handlers still enforce authorization. Hiding a navigation entry is not an
authorization control.

## How the console applies it

One table, `NAV_APPLICATIONS` in
`web/src/console/operator/session-capabilities.js`, maps a navigation entry (an
area, an area tab, or a rail screen) to the application ids that enable it; any
one suffices.

| Navigation entry | Application |
| --- | --- |
| Change requests area | `change-requests` |
| Fleet area | `fleet` |
| Command Center / Release Control Tower; Releases / Plans, Planning | `plans` |
| Releases / Promotion, Waves | `promotions` |
| Packaging / Bundle | `export` or `import` |
| Organization | `teams` |

- An entry with no row stays visible: the Hub publishes no application for it.
  `risk` gates nothing because the console has no risk view.
- An entry with a row is shown only when the session returned one of its ids
  enabled. An id the Hub did not return counts as off.
- While the session read is failing, or answered without an application list,
  mapped entries are withheld, unmapped ones stay, and the rail states
  "Capabilities unavailable" with a Retry. A later failure replaces an earlier
  good answer; the console does not keep navigating on a stale one.
- A direct link to a withheld entry renders a stated "not enabled for this
  session" (or "capabilities unavailable") panel in place of the view.
- The session is re-read whenever the workbench's Hub read model is replaced,
  which is its existing refresh cycle.

`canPerform(operation, resourceRid)` answers
`{ allowed: true | false | null, reason }` from `globalOperations` for
`platform/root` and from `resources` otherwise. `null` means the session did not
report that resource: the console does not guess, adds the resource to the next
session read, and leaves the control enabled for the Hub to decide. A control is
disabled only on `false`, with `reason` as its stated cause.

The maintenance-window scheduler asks for `change:propose` on the selected
`environment/<id>`, matching the temporary-maintenance service. An explicit
refusal disables preview, confirmation, and submission with the session's
reason. It checks the current decision again before sending; editing or
confirming a draft cannot clear a refusal. A permitted preview remains a dry
run, and submission still requires confirmation of that exact reviewed draft.

An index that came back empty is labelled "You have no grants here" when the
session lacks `resource:read` at `platform/root`, because the Hub filters each
index by grant before counting and such a session cannot tell an empty Hub from
a filtered one. A session that holds that read sees the ordinary empty state.
The label is not shown for an index whose read failed.

`scripts/verify-console-contract.mjs` holds the console's application list to
the ids in `internal/controlplane/session.go` in both directions and rejects a
navigation row naming an id the Hub never returns.

The current application vocabulary is `change-requests`, `fleet`, `plans`,
`promotions`, `teams`, `risk`, `export`, and `import`. `change-requests`,
`plans`, and `teams` are enabled for any operator principal; `fleet`,
`promotions`, `risk`, `export`, and `import` read platform-scoped collections
and are enabled only when the principal holds `resource:read` at
`platform/root`. This list is a Conduit projection and is not evidence of a
private Apollo capability protocol.

Source anchors: `internal/controlplane/session.go`,
`internal/controlplane/session_test.go`,
`web/src/console/operator/session-capabilities.js`,
`web/src/console/operator/hub-vocabulary.js`,
`web/src/test/unit/session-capabilities.test.ts`, and
`web/src/test/e2e/session-capabilities.spec.ts`.

Drift-repair controls use `change:propose` on the installation's environment.
The environment hub and retained fleet renderer show the refusal inline and
on disabled controls; the action rechecks before sending. The Hub enforces the
same operation before reading drift details or opening a reviewed request.
The retained fleet renderer is covered as a browser component; it is not a
claim that the current navigation mounts that legacy view.

Intelligence course staging uses `change:propose` on the exact selected
`installation/<environment>/<installation-id>`, matching the course opener.
Changing a target clears confirmation and recalculates the decision. A refusal
is shown inline and on the disabled button; submission rechecks it. Passing a
scored memory gate does not grant proposal permission or approval.
