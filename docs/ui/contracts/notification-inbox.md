# Notification inbox contract

The console's `/console/notifications` screen reads what the Hub recorded for
the calling principal and edits the preference that decides what it records
next. It is a **Conduit extension**: the pinned Apollo OpenAPI under
`docs/reference-platform/snapshots/` publishes no notification operation — the word does not
appear in it — so no shape on this screen is claimed to be Apollo's. The routes
are registered as `notifications.HTTPSurface` in
`docs/evidence/conduit-inventions.json`.

## What the console reads

| Route | Used for |
| --- | --- |
| `GET /v1/notifications/inbox?channel=…` | the inbox table |
| `GET /v1/notifications/preferences?resourceRid=…&channel=…` | the preference editor |
| `PUT /v1/notifications/preferences` | saving an edit |

`GET /v1/notifications/events` is **not** consumed. It answers "what did the Hub
raise about this resource, subscribed to or not", which is a different question
from "what was I told" and needs a resource-first screen to be worth reading;
adding it to the inbox would put rows an operator was never sent beside rows
they were.

## Properties this screen is required to preserve

**An inbox is per-principal, and the console never names an owner.** `ownerKind`
and `ownerId` default to the caller and are exactly the parameters the Hub
refuses when they name somebody else. The console sends neither, on a read or a
write, and reports the owner the Hub resolved rather than one it assumed.
Reaching a team inbox is therefore not yet a console capability.

**A channel is required and is not defaulted by the Hub.** `requestedChannel`
refuses an absent or unknown channel with a 400 rather than choosing one. The
console states a channel, owns it in the URL as `?channel=`, defaults to
`IN_APP` — `domain.NeedsTransport` makes the in-app inbox row its own delivery,
so it is the one channel with content in a deployment that has configured no
transport — and falls back to that default rather than forwarding a channel the
route would refuse. The supported list is EMAIL, IN_APP and SLACK; SLACK is
a registered Conduit preference extension (`notifications.SlackChannel`), with
URL persistence and Hub request coverage in the browser suite.

**A read is bounded and the response reports the bound that was applied.**
`application.Bound` turns every request into at most `MaximumLimit` (100), and
the envelope's `limit` is the applied value, not the requested one, precisely so
a caller can tell a truncated page from the end of the inbox. A page that filled
its limit says so. A page that did not say nothing.

**Empty, loading, failed and refused are four different screens.** An empty
inbox arrives as `{"entries":null}`, because Go encodes an empty slice as null;
it renders as an empty inbox, not as an error and not as a crash. A failed
request renders through `QueryState` as a failure with the Hub's own message and
a retry, and never as an empty inbox — `docs/operations/PRODUCTION-READINESS.md`
records a prior defect where a failed request read as a degraded platform, and
the same mistake pointed the other way is a failed request reading as "nothing
happened". A 403 renders as a refusal. A 404 on a preference is
`application.ErrNoPreference` and is a *state* — "you are not subscribed to
this" — so it renders as a state.

**The event-type vocabulary is open, and the console does not close it.** The
checkbox list is the set this Hub accepts, held to the `domain.EventType`
consts by `scripts/verify-console-contract.mjs` in both directions. Growth
beyond it is what `allFutureTypes` is for. An inbox entry whose type this build
has never heard of still renders, because a row shows the string the Hub sent,
and a stored preference naming such a type is carried through a save unchanged
and reported rather than silently dropped.

**A known type is not necessarily a raised one.** `GET` and `PUT
/v1/notifications/preferences` answer with an additional
`typesNotYetEmitted` array naming those of the preference's `types` that no
emit site in this build raises (`domain.EventType.Raised`). The field is absent
when every named type is raised. Such a type is still accepted and stored — the
checkbox list is the vocabulary, not the set of live emitters — so the field is
how a subscriber learns that silence on that type means "nothing raises it",
not "nothing happened". The console does not yet render it.

**An edit the Hub would refuse is refused first.**
`domain.Preference.Validate` rejects a preference that enables no types and has
not opted into future ones, because it could never deliver anything. The Save
control is disabled in that state and says why, rather than being a button whose
only outcome is a 422.

## Deliberate omissions

- **No unread state and no "mark as read".** `domain.InboxEntry.ReadAt` exists
  and no route writes it. A control here would silently do nothing.
- **No team inbox and no team preference.** The API stores team
  preferences and contacts, and native delivery honors their selected contact
  IDs, but this screen only edits the calling user's preference.
- **No notification count on the rail.** Nothing serves an unread count, and a
  badge computed from a bounded page would be wrong exactly when it mattered.

Source anchors: `internal/notifications/domain/notifications.go`,
`internal/notifications/application/emitter.go`,
`internal/notifications/adapters/httpapi/httpapi.go`,
`web/src/api/notifications.ts`,
`web/src/features/notifications/NotificationsPage.tsx`, and
`web/src/test/e2e/notifications.spec.ts`.
