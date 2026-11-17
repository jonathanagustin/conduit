# Notification inbox contract

The console's `/console/notifications` screen reads what the Hub recorded for
the calling principal, edits the preference that decides what it records next,
and edits the contacts a preference can select. The path lands on the
workbench's `ntf-inbox` screen (the workbench routes by hash, so the same
screen is also `/console/#ntf-inbox`). It is a **Conduit extension**: the pinned Apollo OpenAPI under
`docs/reference-platform/snapshots/` publishes no notification operation — the word does not
appear in it — so no shape on this screen is claimed to be Apollo's. The routes
are registered as `notifications.HTTPSurface` in
`docs/evidence/conduit-inventions.json`.

## What the console reads

| Route | Used for |
| --- | --- |
| `GET /v1/notifications/inbox?channel=…&limit=…` | the inbox table, and the top-bar new count (`IN_APP`, limit 100) |
| `GET /v1/notifications/preferences?resourceRid=…&channel=…` | the preference editor, and the re-read before a save |
| `PUT /v1/notifications/preferences` | saving a preference |
| `GET /v1/notifications/contacts` | the contact list, and the preference editor's contact choice |
| `PUT /v1/notifications/contacts` | adding or replacing the caller's contact of one kind |
| `DELETE /v1/notifications/contacts?kind=…` | deleting it, after a second confirming click |

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
its limit says so; a page that did not is stated to be the whole inbox. The
route has no cursor, so the screen does not page: it asks for 25, offers to
widen to 50 and then to 100, and at the Hub's maximum says that older entries
may exist and are not reachable here. A retry after a failure repeats the limit
that failed rather than widening it.

**Empty, loading, failed and refused are four different screens.** An empty
inbox arrives as `{"entries":null}`, because Go encodes an empty slice as null;
it renders as an empty inbox, not as an error and not as a crash. A failed
request renders as a failure with the Hub's own message and a retry, and never
as an empty inbox — `docs/operations/PRODUCTION-READINESS.md`
records a prior defect where a failed request read as a degraded platform, and
the same mistake pointed the other way is a failed request reading as "nothing
happened". A 403 renders as a refusal. A 404 on a preference is
`application.ErrNoPreference` and is a *state* — "you are not subscribed to
this" — so it renders as a state.

**The event-type vocabulary is open, and the console does not close it.** The
checkbox list is the vocabulary this Hub knows, held to the `domain.EventType`
consts by `scripts/verify-console-contract.mjs` in both directions. Growth
beyond it is what `allFutureTypes` is for. An inbox entry whose type this build
has never heard of still renders, because a row shows the string the Hub sent,
and the browser carries a stored unknown selection into the proposed edit
unchanged rather than silently dropping it. Hub write validation remains
authoritative; a type unknown to the server is refused with 422.

**A known type is not necessarily a raised one.** `GET` and `PUT
/v1/notifications/preferences` answer with an additional
`typeAvailabilityScope: "ALL_KNOWN"` marker and a `typesNotYetEmitted` array
naming the complete known set that no emit site in
this build raises (`domain.EventType.Raised`). A missing preference's 404
response also carries it, so the editor can disable those choices before a
first save. Saving an explicit selection of an unavailable type returns 422;
legacy stored preferences remain readable so they can be repaired. The console
renders each unavailable type disabled and labelled "Not yet emitted by this
Hub", with a Remove control for a saved selection. Opting into all future types
keeps its forward-consent meaning and does not imply an emitter exists today.
An older Hub response without the complete-scope marker disables preference
edits even if it carries a partial `typesNotYetEmitted` list for saved choices.
The editor explains that the Hub must report its supported emitters first and
requires complete coverage again during the pre-save re-read, so a different
older replica cannot silently admit a selection the first response refused.
This availability and write policy is a Conduit extension; the captured Apollo
vocabulary does not establish server-side emitter availability.

**An empty selection can record explicit off consent.** Saving `types: []`
with `allFutureTypes: false` turns off future notifications for the same
(resource, owner, channel) tuple. The stored ID and creation time remain stable,
and the revision advances. It can be loaded and subscribed again later; it is
distinct from a missing preference's 404. Removing a sole legacy unavailable
selection can therefore be saved without opting into other notifications.
The editor identifies off consent before Save and retains its loaded contact.
An inactive team preference needs no contact; active team consent still does.
This is a registered Conduit policy, not a recovered private Apollo API rule.

Off consent affects subsequent fan-out using the updated preference. It does
not delete historical inbox entries or cancel an audience already selected,
recorded or queued for delivery. Empty `types` with `allFutureTypes: true`
retains its existing forward-consent meaning. Unknown named types and missing
owner/resource authority remain refused. A 422 the console did not predict is
shown with the Hub's message.

**A save is explicit, and is refused locally when the preference moved.** The
editor sends nothing until Save, shows what the save would add and remove, and
sends no owner and no revision: the route resolves the first and rejects unknown
fields. The route therefore has no compare-and-set. The console re-reads the
preference immediately before the write and refuses to save when the stored
`revision` differs from the one it loaded. That narrows a lost update to the
interval between the two requests; it does not close it.

The controller binds a Save to the resource, channel, loaded revision, draft,
and operator identity reviewed when the button was pressed. Changing that
scope, editing the draft, navigating away, or disposing the console cancels
the pending request. A late reply cannot update a replacement editor, even if
the transport ignores cancellation. Edits made during a save remain unsaved
until another explicit Save. If the write had already started, cancellation
cannot establish whether it committed: the editor asks for a reload and never
replays the write automatically.

**Save is disabled when the session says the write would be refused.** Reading
or writing a preference needs `resource:read` on its resource. The editor asks
`GET /v1/session?resourceRid=…` for that resource and disables Save with the
reason when the operation is absent. When the session does not report the
resource, the control stays enabled and the Hub decides. Contacts belong to
their owner rather than to a resource, so no operation gates them.

**Environment identifiers share authorization, not storage keys.** Existing
Environment events and preferences retain their opaque `EnvironmentRID` values.
For authorization, the Hub forward-matches that value against its durable native
Environment catalog and checks `resource:read` at `environment/<id>`. This lets an
ordinary native Environment viewer subscribe, read events and receive new
notifications without a second grant on an opaque ID. HTTP, background delivery
and the requested-resource capabilities in `GET /v1/session` use the same
resolver. Capability responses keep the queried opaque key; this metadata does
not change authorization on mutating routes. Unknown, invalid or ambiguous
aliases fail closed; `Event.Environment` cannot confer authority. Native scopes remain
unchanged, and no event, subscription, inbox or delivery identity is migrated
or backfilled. This rule is the registered Conduit extension
`notifications.EnvironmentAuthorizationAlias`.

**A contact the Hub will never deliver to is labelled.** `deliverable: false`
(a PAGERDUTY contact today) renders as "Stored, never delivered".

## The new count

The top bar shows how many `IN_APP` entries are new. No notification event
reaches the console on `GET /v1/events` (`api/live-events/event.schema.json`
has no such kind), so the count is re-read on the workbench's existing refresh
cycle rather than pushed.

"New" is a browser-side mark, not Hub state: `domain.InboxEntry.ReadAt` exists
and no route writes it. An entry is new when it occurred after the newest entry
this browser last showed in the in-app inbox, kept in `localStorage` per owner
the Hub resolved. A `readAt` the Hub does send wins. Because the count comes
from one bounded page, a filled page whose every row is new is shown as "100+",
never as an exact number, and a failed read is shown as "?", never as zero.

## Deliberate omissions

- **No "mark as read" and no cross-device read state.** Nothing on the Hub
  stores it; a control here would silently do nothing elsewhere.
- **No team inbox and no team preference.** The API stores team
  preferences and contacts, and native delivery honors their selected contact
  IDs, but this screen only edits the calling user's.

Source anchors: `internal/notifications/domain/notifications.go`,
`internal/notifications/application/emitter.go`,
`internal/notifications/adapters/httpapi/httpapi.go`,
`web/src/console/operator/notifications.js`,
`web/src/console/operator/hub-vocabulary.js`,
`web/src/test/unit/notifications.test.ts`, and
`web/src/test/e2e/notifications.spec.ts`.
