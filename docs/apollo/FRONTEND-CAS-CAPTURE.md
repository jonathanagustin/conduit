# `apollo-frontend-cas` frontend closure evidence

An OCI artifact holding a content-addressed capture of a live Foundry tenant's
served frontend bytes. Pulled and verified 2026-08-19. The bytes are not
committed; this receipt preserves the exact identity, the findings, and the one
finding that is load-bearing for the console.

| Field | Value |
| --- | --- |
| Artifact | `ghcr.io/jonathanagustin/apollo-ui/apollo-frontend-cas:6.533.15` |
| Manifest media type | `application/vnd.oci.image.manifest.v1+json` |
| Artifact type | `application/vnd.foundry.tree.v1+tar` |
| Config | `application/vnd.oci.empty.v1+json` (this is an artifact, not a runnable image) |
| Layer | `application/zstd`, title `apollo-frontend-cas.tar.zst`, 1,987,540 bytes |
| Layer digest | `sha256:a0bba4dcb53136ab2f883e166d4dacaae5c0564986ee070af4a777e0032127ff` |
| Integrity result | The downloaded bytes hash to exactly that digest |
| Created | `2026-08-19T17:45:04Z` |
| Contents | 31 blobs (30 JavaScript, 1 stylesheet), 2 manifests, `PROVENANCE.md`, `UNAVAILABLE-503.txt` |

Pulled through the registry HTTP API rather than a daemon, because there is no
Docker daemon running in a fresh session and none is needed to read an artifact:

```bash
TOK=$(curl -sS -u "$USER:$GITHUB_TOKEN" \
  "https://ghcr.io/token?service=ghcr.io&scope=repository:jonathanagustin/apollo-ui/apollo-frontend-cas:pull" \
  | sed -E 's/.*"token":"([^"]+)".*/\1/')
curl -sSL -H "Authorization: Bearer $TOK" \
  "https://ghcr.io/v2/jonathanagustin/apollo-ui/apollo-frontend-cas/blobs/sha256:a0bba4dc..." -o layer.tar.zst
sha256sum layer.tar.zst   # must equal the manifest digest before anything is read from it
```

## What it is

The capture's own `PROVENANCE.md` names two frontend batches at version
`6.533.15`:

- **`foundry-frontend-apollo-control-panel`** — complete: the app entry, 29
  scripts (app chunk plus the shared `react` / `blueprint-core` /
  `blueprint-icons` / `security-context` / `graphql` / `intl-messageformat` /
  `polyfills` DLLs, and 13 locale bundles), and one stylesheet.
- **`foundry-frontend-cross-app-debugger`** — shared DLLs only. Its 16 app
  chunks answered `503` from the tenant's own CAS backend with Envoy
  `response-flags: URX`; the exact hashes are listed in `UNAVAILABLE-503.txt`.
  That is a tenant availability fact, not a capture failure, and it bounds any
  claim about the debugger.

## Finding 1 — this is Foundry Control Panel, not the Apollo deployment console

The load-bearing finding, and the reason to read this file before mining the
artifact again.

The app's own title string is
`{platform, select, apollo {Apollo} foundry {Foundry} gotham {Gotham}} <bold>Control Panel</bold>`
— the product is **Control Panel**, and "Apollo" is the platform *skin* selected
by an enrollment. Its 1,037 English `defaultMessage` strings are enrollment
administration: authentication and SAML, email and email suppression and
redaction, network ingress, the deletion queue, markings, extensions, admin
permissions, principal selection.

There is no Release, Product, Plan, Change Request, Recall, Promotion, Spoke, or
Environment-deployment screen vocabulary anywhere in the captured bytes. The
only deployment-adjacent strings are two entries in Control Panel's
application-enablement catalogue: `releaseManagementAppName` = "Release
Management" and `releaseManagementAppDescription` = "Release Management
application" — a row in a list of installable Foundry applications, not a
screen.

This is the same name collision
[`APOLLO-CLIENT-DLL-EVIDENCE.md`](APOLLO-CLIENT-DLL-EVIDENCE.md) records for the
GraphQL client, in a second place: an artifact named `apollo-*` is not evidence
about Palantir Apollo's deployment product. **The Apollo deployment console is a
different frontend app and is not in this capture.** Nothing in Conduit's
Products, Releases, Environments, Plans, Change Requests, Promotions, Fleet,
Risk, Export, or Import screens can be justified by citing this artifact.

## Finding 2 — the Apollo facts it does evidence

All of them are Control Panel administering an Apollo *enrollment*, from the
captured bridge-call and route strings:

| Observed string | What it evidences |
| --- | --- |
| `/admin/enrollment/{enrollmentRid}/restore-apollo-permissions`, `RestoreApolloPermissions.Title` = "Restore Apollo permissions" | Apollo permissions on an enrollment are separately restorable by an administrator |
| `createApolloSpaceForOrganization`, `apolloDescription` = "Bootstrap the enrollment with an Apollo space." | An Apollo enrollment is bootstrapped as a *space* under an organization |
| `/apollo-enrollment-organization`, `/apollo-third-party-applications` | Enrollment-to-organization and third-party-application administration are Apollo-scoped surfaces |
| `/environment/applied-apollo-labels`, `getAppliedApolloLabels` | Environments carry applied Apollo **labels**, read through the Control Panel bridge |
| `Dialog.Title` = "Manage Alerting", `Dialog.Description` = "The alerting status is responsible for enabling or disabling alerting UI in Apollo, such as the NOC Inbox and the monitors tabs." | Apollo's alerting UI is a **NOC Inbox** plus **monitors tabs**, and it is switchable per organization |

The last row is the only description of Apollo deployment-side UI in the whole
capture, and it is second-hand — a Control Panel dialog describing what it
toggles. It is not a screen capture and does not describe those screens' shape.

## Finding 3 — the relative-time contract, which the console now implements

The captured app chunk carries English `defaultMessage` strings inline, so the
`RelativeTime` vocabulary is recovered exactly rather than translated back:

```
RelativeTime.Now          now
RelativeTime.SecondsAgo   {seconds, plural, one {# second} other {# seconds}} ago
RelativeTime.MinutesAgo   {minutes, plural, one {# minute} other {# minutes}} ago
RelativeTime.HoursAgo     {hours, plural, one {# hour} other {# hours}} ago
RelativeTime.DaysAgo      {days, plural, one {# day} other {# days}} ago
RelativeTime.WeeksAgo     {weeks, plural, one {# week} other {# weeks}} ago
RelativeTime.MonthsAgo    {months, plural, one {# month} other {# months}} ago
RelativeTime.YearsAgo     {years, plural, one {# year} other {# years}} ago
RelativeTime.Yesterday    Yesterday at {formattedTime}
RelativeTime.Today        Today at {formattedTime}
RelativeTime.Tomorrow     Tomorrow at {formattedTime}
```

…each with a `*FromNow` twin reading `in {n} {unit}`. The algorithm around them
is legible in the same chunk. Written out, with the minified names kept so the
claim can be re-checked against the bytes:

```js
es=6e4, el=60*es, ec=24*el, eu=7*ec, ed=30*ec, ep=365*ec   // minute, hour, day, week, 30-day month, 365-day year

// |value - now| < 1000  ->  RelativeTime.Now
// past branch, e = now - value:
//   e < es          -> SecondsAgo, Math.floor(e/1e3)
//   e < el          -> MinutesAgo, Math.floor(e/es)
//   dayOffset == -1 -> Yesterday at {formattedTime}          <- named day beats the hour count
//   e < ec || dayOffset == 0 -> HoursAgo, Math.floor(e/el)
//   else            -> em(dayOffset)
function em(e){ let t=-e.offsetFromToday, n=t*ec;
  if(n<3*eu) return DaysAgo(t);
  if(n<2*ed) return WeeksAgo(Math.floor(n/eu));
  if(n<2*ep) return MonthsAgo(Math.floor(n/ed));
  return YearsAgo(Math.floor(n/ep)); }
```

Four properties matter and none of them were in the console's hand-rolled
version:

1. **Truncation, not rounding.** `Math.floor` throughout. 59m59s is "59 minutes
   ago"; the console said "60 minutes ago", an hour that had not happened.
2. **A named day outranks an elapsed count.** The yesterday/tomorrow test runs
   *before* the hour count, and the day offset is a calendar-day difference, not
   elapsed milliseconds.
3. **The ladder continues past days** — weeks at 21 days, months at 60 days,
   years at 730 days. The console stopped at days, so last quarter's release
   read "97 days ago".
4. **Beyond a day the input is whole calendar days**, divided by coarse
   constants (a month is 30 days, a year is 365). That is why a value does not
   slide between units as the clock crosses noon.

`{formattedTime}` is Foundry's `{hour: "numeric", minute: "numeric"}` in the
viewer's zone and locale. The console renders `HH:MM UTC` instead, and compares
calendar days in UTC — a deliberate departure, recorded in
`web/src/components/Timestamp.tsx`, because this console pins UTC everywhere so
two screens cannot disagree about the order of events, and because a
locale-dependent string cannot be pinned by a test.

Implemented in `web/src/components/Timestamp.tsx`; pinned by the "relative time"
block in `web/src/test/unit/logic.test.ts`, which spells the captured boundaries
out as expectations. Four compiling mutants (round instead of floor, drop the
yesterday branch, move the week threshold to 4 weeks, make the month 31 days)
each kill exactly one of those tests.

## Finding 4 — observed, not adopted

- **Blueprint 6 design tokens.** The stylesheet defines `--bp-palette-*`,
  `--bp-intent-*`, `--bp-typography-*`, and `--bp-surface-*` with concrete
  values. The console is on Blueprint 5. This is a real token set to check
  against if the console ever moves, not a reason to move it.
- **Foundry's date-format presets**, e.g. `{day, month: "short", weekday:
  "short", year, hour, minute}` for a full timestamp and `{hour, minute}` for a
  time. The console deliberately shows ISO-8601 UTC in the `title` instead.
- **Core vocabulary** — "Loading…", "Copy to clipboard" / "Copied!", the
  unsaved-changes alert ("Continue editing" / "Discard changes"), and the
  non-ideal states "Access denied" / "Service unavailable" / "Invalid URL". Some
  of these are near-matches for what the console already says. None was changed
  on the strength of this capture alone: Control Panel's wording is Control
  Panel's, and Conduit's screens are not Control Panel's screens.

## Safety

Nothing from the capture is committed. The strings quoted above are served
product bytes; the artifact carries no tenant hostname, enrollment or customer
RID, token, or tenant data, which the capture's own provenance note states and
which re-reading the blobs for the quoted strings did not contradict. The route
templates quoted in Finding 2 contain the literal placeholder
`{enrollmentRid}`, not a RID.
