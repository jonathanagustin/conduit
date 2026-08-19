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

### The sibling artifact, pulled and dismissed

`ghcr.io/jonathanagustin/apollo-ui/apollo-control-panel-frontend:6.533.15`
(manifest `sha256:c18fac2221c9…`, layer `sha256:325c31ee70fd…`, created
`2026-08-19T17:29:12Z`) is named alongside this one and is **a strict subset of
it**. Pulled and diffed rather than assumed: 30 blobs, all byte-identical to
their copies here, missing the `c4219b7e…` `cross-app-debugger` entry script
this artifact carries, and its `entry-manifest.json` payload is byte-equal to
`manifests/apollo-control-panel.json`. It adds **no chunk**, so it does not move
the 31%-coverage bound in Finding 5. Recorded so nobody pulls it twice.

It contributes exactly one fact its provenance note states and this one does
not: the manifests are the tenant's
`x-workspace-application-entry-manifests` payload — the mechanism by which
Foundry hands a browser its entry manifest, with both CAS and versioned URLs.

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

Pinned again in a browser by `web/src/test/e2e/timestamps.spec.ts`, which walks
every rung of the ladder through the rendered Plans table against a fixed clock
and asserts the exact UTC value is carried in `datetime` and `title` beside it.
The unit tests cannot establish that: they call the function, so a `Timestamp`
that stopped rendering its own output would leave all of them green, and before
this spec no e2e test asserted a rendered timestamp anywhere in the console.

## Finding 4 — the console's design tokens are the tenant's, measured

> **Correction.** The first version of this file said "the console is on
> Blueprint 5". That was wrong, and it was inferred from the bare import path in
> `web/src/styles/index.css` rather than read from the installed package.
> `@blueprintjs/core` is **6.18.0**. The capture and the console are on the same
> major, which is what makes the comparison below possible at all.

The captured stylesheet defines **205** `--bp-*` tokens. The installed Blueprint
defines exactly the same 205 — no token in one that is absent from the other —
and every value matches once minifier notation is folded (`#fff` vs `#ffffff`,
`.1s` vs `100ms`, `#0000001a` vs `rgba(0, 0, 0, 0.1)`). **Sixteen** of the 205
differ textually; **zero** differ semantically.

That is now a gate rather than a one-off reading.
[`contracts/foundry-design-tokens.json`](contracts/foundry-design-tokens.json)
holds the captured values, and `scripts/verify-design-tokens.mjs` compares them
against the installed package on demand:

```
PASS design tokens: 205 Blueprint tokens match the values
     foundry-frontend-apollo-control-panel 6.533.15 serves
```

It fails on a real difference — a single hex digit changed in the contract
produces `--bp-intent-primary-rest: console has #2d72d2, the tenant serves
#2d72d3` — and on either side gaining or losing a token. It says nothing about
layout, spacing, component choice or copy, and it is not evidence about the
Apollo deployment console: tokens are shared platform styling, which is exactly
why they transfer when the screens built on them do not.

### The typefaces, one of which the console was getting wrong

The capture settles the faces directly, in its own `@font-face` blocks and
rules:

```
pre,code{font-family:Source-Code-Pro,Courier New,monospace,blueprint-icons-16}
.bp6-monospace-text,...{font-family:Source-Code-Pro,...,blueprint-icons-16!important}
```

…with `source-sans-pro` self-hosted for body text at weights 200/400/600 plus
italics. Two consequences, both applied:

- **Body text was right, its fallback stack was not.** Foundry ends every family
  list with `blueprint-icons-16`, as Blueprint's own default does, so an icon
  glyph rendered as text still resolves. The console's override dropped it. Now
  restored.
- **Code text was wrong everywhere.** Foundry renders digests, versions and
  identifiers in **Source Code Pro**. The console asked for
  `var(--pt-font-family-monospace)` in five files and
  `var(--bp-font-family-monospace)` in two — and Blueprint 6 defines *neither*
  (the real token is `--bp-typography-family-mono`, whose value is bare
  `monospace`). All seven silently fell through to the browser default, and the
  `, monospace` fallback in each declaration is the only reason the screens
  looked deliberate. The console now self-hosts Source Code Pro and names one
  `--console-font-family-monospace` stack, reproducing the captured rule
  including its `!important` on `.bp6-monospace-text`, because Blueprint's own
  rule for `code` hard-codes bare `monospace` rather than reading a token —
  which is exactly why Foundry had to override it too.

## Finding 5 — how far the bundles decode, and the hard bound on this capture

Worth stating precisely, because "we have the bundle" reads as "we have the
app", and here it does not.

- **No source maps.** Zero of the 30 JavaScript blobs advertise a
  `sourceMappingURL`. The original TypeScript is not recoverable; everything
  below is recovered from the shipped bytes.
- **English UI strings: 1,037.** Foundry compiles `{id, defaultMessage}` pairs
  into the chunk, so the English is read directly rather than translated back
  from a locale bundle.
- **But the app has 3,382 message keys.** The 13 locale bundles carry the *whole*
  app's key inventory, translated. Subtracting the English recovered from the
  served chunks leaves **2,511 keys — 74% — with no served English at all**,
  because they belong to lazily-loaded chunks the capture never fetched.
  So this capture covers roughly **31%** of Control Panel's UI strings. Any
  claim of the form "Control Panel has no X" is bounded by that number, and the
  locale bundles are the cheapest way to check whether an absent thing is truly
  absent or merely unfetched.
- **Component structure decodes by name.** CSS-module class names are
  `owner__element__hash`: 26 owning packages and 1,763 element names, led by
  `resource-icon` (687), `control-panel-shell` (257), `deletion-queue` (86),
  `markings` (84).
- **The dependency graph decodes from the manifests.** `featureFlagReferences`
  carries 65 entries across 24 `@foundry/*` packages with real monorepo paths
  (`packages/control-panel/centralized-communications-extension/src/flags/…`),
  including flag identifiers such as `enableCentralizedCommunicationsApollo`.

What none of it recovers: component logic in readable form, the routes of the
unfetched chunks, or any screen of the Apollo deployment console — which
Finding 1 already establishes is not in this artifact at all.

## Finding 6 — the whole API surface, and what it settles

A companion extraction mined the same bundle closure for the API the frontend
talks to, statically and with no live calls. It is committed here rather than
referenced, because its source is a `claude/*` branch that may be rebased or
deleted:

| Artifact | Recovered |
| --- | --- |
| [`apollo-control-panel-rest-endpoints-6.533.15.json`](apollo-control-panel-rest-endpoints-6.533.15.json) | 438 Conjure REST endpoints across 34 services |
| [`apollo-control-panel-graphql-operations-6.533.15.json`](apollo-control-panel-graphql-operations-6.533.15.json) | 24 complete GraphQL operations, 30 fragments, 54 definitions |

Provenance: `jonathanagustin/apollo-ui`, branch
`claude/foundry-ui-review-xzi0eg`, commit `9519522`, files
`contracts/rest-conjure-endpoints.json` and `contracts/graphql-operations.json`,
copied byte-for-byte. Their local digests are
`96ffd597a501548881a3451e875ae4a215ae08de1dcaebab6339fccf538f5cb1` and
`e4fa55d375133810b3bb78720e0344b7801b2b4e68bf565c6a53ed898cdef082`.

**Confidence, carried over rather than upgraded.** REST is *bundle-path-only*:
method, endpoint name and templated path come from the generated
`IHttpApiBridge` calls (`bridge.call(service, endpointName, httpMethod,
endpointPath, …)`), and no request or response Conjure bean shape has been
type-verified against a live 2xx. GraphQL is *closure-complete* — every
operation's fragments resolve — but the field and argument **types** are the
reconstructed document's, not the server schema's, and have not been probed.
Neither may be cited as a verified contract.

**Leak posture, checked here rather than taken on trust.** Scanning both files
for `ri.*` RID literals, Palantir hostnames, JWTs, UUIDs and absolute URLs
returns zero of each. Every path is templated (`{rid}`, `{organizationRid}`,
`{enrollmentRid}`).

### It corroborates Finding 1 from the other end, and much harder

Finding 1 argues from UI strings that this is Control Panel. The API surface
argues the same thing from the wire, at a scale that is difficult to explain
away — **438 endpoints, and not one deployment endpoint**:

```
CompassService 49 | OrganizationAndProjectService 41 | EnrollmentManagementService 34
MandatoryControlsService 28 | AuthenticationProviderService 26 | NamespaceAndProjectService 26
UserDirectoryService 21 | CustomerManagedDomainService 20 | TaskRequestService 20
IngressConfigurationService 19 | WorkspaceService 17 | CommentService 14
GroupAdministrationService 14 | OrganizationManagementService 14 | DomainService 11
EnrollmentService 11 | DeletionContextService 10 | OrganizationPermissionService 7
OrganizationTriagingService 6 | ScopedSessionDefinitionService 6 | CbacBannerService 5
EnrollmentSetupManagementService 5 | OrganizationService 5 | SearchService 5
ConfigurationService 3 | CorsConfigurationService 3 | EnvironmentService 3
PathService 3 | ShareService 3 | UserSettingsService 3
DescriptionService 2 | ScopedSessionSelectionService 2 | EdgeControlPanelService 1
TokenService 1
```

Compass, enrollment, organization, identity, mandatory controls, networking.
There is no Product, Release, Plan, Change Request, Promotion, Recall or Spoke
service. Scanning all 438 for deployment vocabulary
(`release|product|install|deploy|plan|spoke|recall|rollout|artifact|helm|chart|version|upgrade|promot|maestro|agent`)
returns **four** hits, and each is a false friend worth naming so the next
reader does not have to re-check them:

| Hit | What it actually is |
| --- | --- |
| `TaskRequestService.getBatchTaskRequestVersionDetails` `/task-requests/batch/version` | a revision of a task request |
| `TaskRequestService.createRequestVersion` `/task-requests/{taskRid}/version/create` | the same |
| `WorkspaceService.getAssetsInstalledOnTrack` `/installedAssets` | **frontend** SLS assets on a release *track* — the mechanism that served this very bundle, per the `staticUrl` shape in `manifests/`, not Apollo Entity installation |
| `WorkspaceService.getAssetRoleInfoInstalledOnTrack` `/assetRoleInfo` | the same |

`EnvironmentService` — the one service whose name suggests deployment — has
exactly three endpoints, all reads: `getAppliedApolloLabels`
(`/environment/applied-apollo-labels`), `getAvailablePlatforms`, and `isItar`.

The GraphQL side agrees: the deployment-vocabulary scan over the entire
operation extract returns **zero**. The 24 operations use these root fields —

`me` (2), `enrollment`, `principal`, `resourceMetadata`, `sessionClassification`,
`authenticationProvider`, `stargateUserDirectory` (4), `stargateUser`,
`currentStargateUser` (2), `stargateIsPalantirUser`, and the Stargate
create/update/delete/reset/passkey/OTP/enable mutations

— and **no `apollo` root field**. What that does and does not settle for
Conduit's GraphQL work is recorded in
[`reference/graphql-api.md`](reference/graphql-api.md), under the captured
gateway comparison, because that is where the open conflict lives.

### Two independent sources agree, three for three

The extraction's own confidence label is "bundle-path-only, not type-verified",
and it stays that. But this repository already holds the *other side of the same
seam*: [`../evidence/apollo-control-panel-seam.json`](../evidence/apollo-control-panel-seam.json)
is a bytecode-exact projection of the published Maven artifact
`com.palantir.controlpanel:control-panel-api-objects:1.1588.0`, captured
2026-08-17, at **Primary** fidelity for operation names, HTTP shapes and field
types. It declares exactly three Apollo-named operations. The frontend closure,
mined two days later from shipped bytes by an unrelated method, calls exactly
those three — and every field agrees:

| Operation | Maven projection (server) | Frontend closure (client) |
| --- | --- | --- |
| `createApolloSpaceForOrganization` | `POST /control-panel/api/admin/enrollment/{enrollmentRid}/organization/{organizationRid}/create-apollo-space` — `EnrollmentManagementService` | `POST /admin/enrollment/{enrollmentRid}/organization/{organizationRid}/create-apollo-space` — same service |
| `restoreApolloPermissions` | `PUT /control-panel/api/admin/enrollment/{enrollmentRid}/restore-apollo-permissions` — `EnrollmentManagementService` | `PUT /admin/enrollment/{enrollmentRid}/restore-apollo-permissions` — same service |
| `getAppliedApolloLabels` | `GET /control-panel/api/environment/applied-apollo-labels` — `EnvironmentService` | `GET /environment/applied-apollo-labels` — same service |

Same service, same method, same path — the client paths differ from the server
paths by exactly the constant `/control-panel/api`, which the generated
`IHttpApiBridge` supplies and the bundle therefore never carries.

That is worth more than three rows. It establishes what the other 435 paths
*are*: service-relative under `/control-panel/api`, not fragments of unknown
origin. It does not make them type-verified — nothing here says anything about
request or response beans — but "the path shape is confirmed against a Primary
source wherever the two overlap" is a materially different claim from "mined
from a minified bundle and unchecked".

Pinned by `scripts/verify-apollo-frontend-surface.mjs`, which fails on a
disagreement in service, method or path, on an operation the projection declares
and the frontend has lost, on a count that stops matching its contents, on a RID
literal appearing in a templated path, and on a **new** deployment-vocabulary
endpoint the four recorded false friends do not account for — so Finding 1 goes
stale loudly rather than quietly. Its self-test plants all six.

### One thing it does not recover: which endpoint the client posts to

Worth stating because it looks recoverable and is not. The only `/graphql`
string in the closure is Apollo Client's own library default (`{uri = "/graphql"}`
in `HttpLink`'s constructor), not the app's configuration; there is no
`graphql-gateway` string anywhere in the 30 blobs and no `uri:` literal in the
app chunk. The endpoint is assembled at runtime. What the bytes *do* name is
the client: requests carry `Fetch-User-Agent: … forge-graphql-client/0.0.0`,
a `Palantir-Asset-Track` header and a bearer token.

### No Conduit code follows from this

Recorded deliberately. Every one of the 438 endpoints is Foundry platform
administration — Compass resources, enrollments, organizations, directories,
ingress. None is an Apollo public-profile route Conduit serves or should serve,
and none is a console obligation. The value here is evidential: it closes off a
line of inquiry rather than opening one.

## Observed, not adopted

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
