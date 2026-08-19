# Apollo Hub frontend hydration, and what Conduit's console bootstrap should do

This note answers one question that was asked of the evidence rather than of a
design instinct: **how does Apollo Hub supply runtime and tenant configuration
to its frontend, and what should change in Conduit's console bootstrap as a
result?**

The short answer to the first half is that the published Apollo contract does
not describe it, and this repository holds no authorized observation of an
Apollo Hub browser shell booting. The short answer to the second half is that
`conduit.console.bootstrap/v1` should not change. The reasoning for both is
below, because a recommendation of "change nothing" is only worth anything if
the search that produced it is legible enough to be re-run.

## What was searched, and what it returned

The pinned public contract is
`docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml`,
SHA-256 `9e52d7fe6edb29c2f8020ac1179c582cdc89163ccb83a8c17dd803c4f651ed80`,
upstream commit `c663874bd7467a01e719d039509b0f206d05fe2c` of
`palantir/apollo-openapi`. It declares thirteen paths and every one of them is
under `/api/v0`: product publication, products, releases, environments,
installations, entity info and health, and namespaces. There is no
configuration endpoint, no bootstrap endpoint, no session or "me" projection,
no service-discovery document, and nothing served to a browser shell.

That absence was checked rather than assumed. A case-insensitive search of the
definition for `hydrat`, `config`, `bootstrap`, `/me`, `session`, `frontend`,
`browser`, `console`, `brand`, `theme`, `tenant`, `discovery`, `capabilit`,
`feature`, `toggle`, `login`, `logo`, `whoami`, `settings`, `preference`, and
`locale` returns zero matches for every one of them. The single term that
appeared to match, `ui`, has thirty occurrences and all thirty are the word
`required`; that was resolved by extracting the matched substrings rather than
by reading the count, because a count is exactly the kind of number this
repository has been wrong about before.

The nearest surface in the snapshot is `listNamespaces`
(`GET /api/v0/namespaces`), described as "Lists namespaces the user has access
to". It is a per-caller authorization projection, which is the same *shape* as
something a frontend might read at boot. It is not a hydration document: it is
an authenticated `OAuth2`/`BearerAuth` API call returning namespace ownership
groups, and nothing in the snapshot connects it to a browser shell. Treating it
as evidence of hydration would be the inference this repository forbids.

The wider Apollo corpus under `docs/apollo/` was searched the same way. The
string `hydrat` does not occur anywhere in it. `index.html`, `meta tag`,
`single-page`, `web app`, `whoami`, `login page`, `branding`, `white-label`,
and `favicon` do not occur either. Every occurrence of `bootstrap` in that
corpus refers to something else entirely — Flux and namespace reconciliation in
`docs/apollo/reference/namespaces.md`, permission-administrator bootstrap in
`docs/apollo/reference/identity-access.md`, and a bootstrap gate on unknown
Environment state in `docs/apollo/IMAGE-DERIVED-GAPS.md`. None of them is a
frontend.

Two findings are genuinely about Apollo's frontend, and both are worth stating
precisely because both stop short of hydration.

`docs/apollo/ROSETTA-RELEASE-NOTES.md` establishes that Apollo's web console is
the surface named `autopilot`, that `apollo-build-factory-app` carries console
panels and Hub settings tabs, and that 49 of 55 observed release notes are
UI-surface notes carrying no contract Conduit could conform to. This tells us
the console exists and what it is called. It says nothing about how it is
configured at boot.

`docs/apollo/reference/graphql-api.md` is the closest the corpus comes, and it
is the most important document here. It records, against the official Apollo
API Explorer documentation reviewed 2026-08-03, that Palantir publishes this
example selection:

```graphql
query GetCurrentUser {
  me { id fullName }
}
```

That is a session projection, and it establishes something narrow but real:
where Apollo's own documentation shows a client learning who the caller is, it
does so through an **authenticated GraphQL query**, not through a document
injected into HTML. The same note is explicit that the documentation publishes
no universal Explorer pathname, no endpoint, no downloadable SDL, no
authentication fixture, and no complete schema, so the examples "prove only
their named fields and cannot support a broader compatibility claim." It also
records that the one authorized capture attempted against a real tenant — the
Foundry Connected Hubs route, 2026-08-03T04:43:08.453Z, manifest SHA-256
`8bef34e33677617ff6a922594a91a1b0b7249be892f6b993faff8d3e37ac99ee` — was
blocked by a plan gate reporting "Excluded from your plan", and that of the
five GraphQL request shapes captured, "none is an Apollo Hub operation."

So the one attempt to observe an Apollo Hub frontend from an authorized tenant
returned a blocked discovery path. That is evidence of a blocked path, not
evidence of an absent capability, and it is the reason the honest classification
below is `Unknown` rather than "Apollo has no hydration."

There is one more trap in this corpus that this note repeats deliberately,
because it is the failure mode a future reader is most likely to walk into.
`docs/apollo/APOLLO-CLIENT-DLL-EVIDENCE.md` records a frontend asset literally
named `apollo-client-dll`, captured from an authenticated tenant's
content-addressable store at SHA-256
`2804ee8a6a0c9f5cc6b711cc345421adfaac324cb89a69f7a2648583e9f0026c`. It is the
open-source Apollo **GraphQL** client, `@apollo/client` 4.1.2. It is not
Palantir Apollo. As that receipt puts it, "The shared word 'Apollo' is a name
collision that must not merge the two evidence tracks." Anyone searching this
repository for Apollo frontend evidence will hit that file early, and it
establishes nothing about Palantir Apollo's console.

## Evidence table

| Claim | Class | Immutable source | Scope not established |
| --- | --- | --- | --- |
| The public Apollo API declares 13 paths, all under `/api/v0`, and none is a configuration, bootstrap, session, or discovery endpoint | Public contract | `docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml`, API SHA-256 `9e52d7fe6edb29c2f8020ac1179c582cdc89163ccb83a8c17dd803c4f651ed80` | Whether private, undocumented Hub routes serve a frontend. The snapshot is the *public* API and says so. |
| `GET /api/v0/namespaces` (`listNamespaces`) returns the namespaces the calling user can access | Public contract | Same snapshot, path `/api/v0/namespaces` | That any frontend reads it at boot. Nothing connects this operation to a browser shell. |
| Apollo documents a `me { id fullName }` GraphQL query as the published way a client learns the current user | Public contract | `docs/apollo/reference/graphql-api.md`, reviewed 2026-08-03 against Palantir's Apollo API documentation | The endpoint path, transport, SDL, auth fixture, or whether this is read during frontend boot. The source states none is published. |
| Apollo's web console is the surface `autopilot`; 49 of 55 observed release notes are UI-surface only and carry no conformable contract | Tenant observation | `docs/apollo/ROSETTA-RELEASE-NOTES.md`; registry `docs/apollo/observed/apollo-rosetta-release-notes.json` | How `autopilot` is configured at boot. No note in the registry describes console configuration, branding, or tenant loading. |
| An authorized capture of a live tenant's Connected Hubs route exposed no Apollo Hub origin and no API Explorer; the plan gate blocked the extension | Tenant observation | `docs/apollo/reference/graphql-api.md`, capture manifest SHA-256 `8bef34e33677617ff6a922594a91a1b0b7249be892f6b993faff8d3e37ac99ee`, accessibility tree SHA-256 `1e3c7a8f9df6bdcb916880c4360ee6c760116b5d5f502040e9ee6e31dc0ad8b6`, 2026-08-03T04:43:08.453Z | Anything about Apollo Hub hydration. This is a blocked discovery path and must not be read as an absence result. |
| **How Apollo Hub supplies runtime or tenant configuration to its frontend** | **Unknown** | No source in this repository. The pinned snapshot, the reference contracts, the observed registries, and the media audit are all silent. | Everything. There is no public contract, no authorized observation, and no corroborating capture of an Apollo Hub console boot. |
| The `apollo-client-dll` frontend asset is the open-source `@apollo/client` GraphQL library, not a Palantir Apollo delivery client | Tenant observation | `docs/apollo/APOLLO-CLIENT-DLL-EVIDENCE.md`, download SHA-256 `2804ee8a6a0c9f5cc6b711cc345421adfaac324cb89a69f7a2648583e9f0026c` | Anything about Palantir Apollo. Recorded here only to stop a name collision from being mistaken for evidence. |
| Apollo injects an authoritative `apollo` value block into Helm values that user configuration cannot replace | Public contract | `docs/apollo/reference/configuration.md`, sources indexed in `docs/apollo/reference/review-ledger.tsv` | That this pattern extends to browser configuration. It governs workload config, not a frontend. It is cited below only as an argument by analogy, and is labelled as such. |
| Foundry (a different product) hydrates a browser shell through `<meta name="x-workspace-page-environment-data">` carrying ~40 service endpoint URLs, of which the Compass bundle consumes two | Corroborating | `/workspace/foundry/apps/compass/src/runtime-config.ts`; evidence receipt named in that file as `evidence/artifacts/tenant-hydration-mechanism-2026-07-31.json` (H42) | Anything about Apollo. Foundry is a different Palantir product and its practice is not an Apollo contract. |
| Foundry's *pre-authentication* hydration carries only `instructions`, `logoUri`, `tabIconUri`, `tabTitle`, plus a `userDirectoryId` route parameter; every property optional and nullable | Corroborating | `/workspace/foundry/tenant/example/config/login-configuration.schema.json` and `hydration.schema.json` | Anything about Apollo. Also note the containing repository is a *reconstruction* of Foundry, not Palantir source, as `docs/ui/foundry-frontend-architecture.md` states. |
| Foundry keeps feature flags in a separate, opt-in file contract deliberately omitted from the canonical document, so nothing is enabled by default | Corroborating | `/workspace/foundry/tenant/example/config/README.md` | Anything about Apollo. |
| Conduit's console and API are served from one origin; the client issues relative `/v1` requests with `credentials: "same-origin"` and handles no token | Conduit extension | `web/src/api/client.ts` | — |
| `GET /console/` and `GET /console` are registered `DecisionPublic`: the shell is served to an unauthenticated caller | Conduit extension | `internal/hub/composition/authorization.go` | — |
| `GET /v1/session` is registered `DecisionOperatorSelf` and is the console's authority for identity and application navigation | Conduit extension | `internal/hub/composition/authorization.go`; `docs/ui/contracts/session-capabilities.md`; `internal/controlplane/session.go` | Any correspondence to a private Apollo capability protocol. The contract document already disclaims this. |
| Both bootstrap validators reject unknown *and* missing fields and compare `schemaVersion` by exact equality | Conduit extension | `internal/consoleui/bootstrap.go`; `web/src/runtime/bootstrap.ts` | — |

## What is not established

Stated plainly, because the temptation to round this up into a story is the
main risk in the whole exercise:

- **The published Apollo contract does not describe frontend hydration.** There
  is no Apollo bootstrap document, no Apollo runtime-configuration endpoint, no
  Apollo boot-time session read, and no Apollo service-discovery surface in any
  source this repository holds.
- **No authorized observation of an Apollo Hub console boot exists here.** The
  one capture attempted was blocked by a licensing gate before it reached a Hub.
- **`me { id fullName }` is not established as a hydration mechanism.** It is a
  documented query. Whether `autopilot` calls it at boot, after login, or at
  all, is unknown.
- **Nothing here establishes that Apollo does *not* hydrate a frontend.** A
  blocked discovery path is not a negative result, and the snapshot is
  explicitly the public API only. Any future claim in either direction needs a
  new authorized capture, not a re-reading of these files.

Under `docs/operations/TENANT-FIDELITY.md` and ADR 0007, `Unknown` no longer
means stop — it means build against a disclosed local decision and register what
would displace it. That is precisely what
`conduit.console.bootstrap/v1` already is: its provenance field carries
`claimClass: conduit-extension` with a `conduit://` coordinate for the default
document, which is the surface telling the truth about itself. No part of this
proposal needs Apollo evidence it does not have, because no part of it claims
Apollo behavior.

## What should change in Conduit's console bootstrap

Nothing. The three sub-questions are answered below, and in each case the
existing design is either already correct or already better than the
alternative.

### A runtime API-origin field is not needed, because same-origin serving settles it

`web/src/api/client.ts` sets `const BASE = "/v1"`, builds every request URL
against `window.location.origin`, and passes `credentials: "same-origin"`. Its
comment already states the reason: "The Hub serves the console and the API from
one origin, so requests are relative and credentials ride along." There is no
second service to point at, no gateway on another host, and no token for the
client to carry.

This is the one place where Foundry's practice is instructive rather than
merely adjacent, and the instruction runs *against* copying it. Foundry's
Compass bundle does hydrate an API base URL, mapping `compassApi` to
`apiBaseUrl` and `graphqlGatewayApi` to `graphqlEndpoint` out of a blob of
roughly forty service endpoints. It needs to, because Foundry is many services
behind a deployment-specific proxy and the bundle cannot know where the GraphQL
gateway is mounted. Conduit is one binary serving one origin. Adding an
API-origin field would import Foundry's problem without importing Foundry's
architecture, and it would do so at real cost: a tenant-supplied origin in a
document served to an unauthenticated caller is a redirection primitive, since
whoever writes the document chooses where an authenticated operator's console
sends its next request. `docs/ui/contracts/runtime-bootstrap.md` already records
"external API origins" among the things the negative tests reject. That test is
protecting something.

Worth noting for the record: the same Foundry runtime config declares optional
`token` and `graphqlToken` fields, commented "omitted for same-origin cookie
auth." A credential-shaped field that is merely *conventionally* absent is
exactly the field Conduit's closed validator makes structurally impossible.
Conduit's version is the stricter one and should stay that way.

### Nothing capability-shaped belongs in the injected document, and the better seam already exists

This is the question that matters most, and it turns out to have been decided
already — correctly — and merely not to have been written down as an answer to
*this* question.

The decisive fact is in `internal/hub/composition/authorization.go`:
`GET /console/` is registered `DecisionPublic`, "the console shell and its
assets; every datum it shows is fetched from a route below." The shell is served
to anonymous callers. Whatever the Hub injects into that HTML is therefore
disclosed to anyone who can reach the Hub, before any authentication has
happened and without any principal existing to scope it to. A capability list
in that document would be a pre-authentication disclosure of what the
deployment can do, and — worse — a *per-principal* capability list would be
incoherent there, because at injection time there is no principal. The document
is rendered once at handler construction, not per request.

The right seam is the one already built. `GET /v1/session` is registered
`DecisionOperatorSelf` — `requireOperatorPrincipal` followed by
`projectSession`, "which projects the caller's own standing." Per
`docs/ui/contracts/session-capabilities.md` it returns the actor and effective
principal, delegation state, application IDs with an `enabled` flag, and
operations granted at `platform/root`. It is read after authentication, it is
scoped to the caller, and it is recomputed per request rather than frozen at
process start.

The two seams differ along every axis that matters, which is why splitting them
was right:

| | Injected bootstrap | `GET /v1/session` |
| --- | --- | --- |
| Audience | Anonymous | Authenticated operator |
| Scope | Deployment-wide | The calling principal |
| Freshness | Fixed at handler construction | Per request |
| Authored by | Operator, via a mounted file | Derived from grants |
| Consequence of a wrong value | Wrong branding | Wrong navigation |

Anything whose correct value depends on *who is asking* cannot live in a
document rendered before anyone asked. That is not a policy preference; it is a
property of when the document is built.

Two further guards are already in place and should be left alone. The session
contract states that the console "must not replace a failed session request with
a hardcoded capability set" — it reports capabilities unavailable and leaves
navigation incomplete — which is what stops a degraded session read from
silently falling back to the injected document. And it states that "Hiding a
navigation entry is not an authorization control," with route handlers still
enforcing authorization. Both sentences exist because the alternative is a
console that looks correct while being wrong, and this repository has already
paid for that class of error once: `docs/operations/route-authorization.md`
records a measured finding that a Hub route with no registered decision returned
`200 {"tenant-b-secret":"the crown jewels"}` to an unauthenticated caller. The
fix there was to make the surface closed and fail-closed. The bootstrap
validator is the same fix applied to a document instead of a route, and
widening it would be undoing it.

There is a supporting argument by analogy from Apollo's own contract, offered as
analogy and not as evidence, since `docs/apollo/reference/configuration.md`
governs workload configuration rather than a frontend. Apollo injects an
authoritative `apollo` block into Helm values and, in that document's words,
"User configuration cannot replace this authoritative block." The shape of that
decision — operator-supplied configuration is admitted, but a bounded
platform-owned region of it is not overridable by the tenant — is the same
shape as a closed console bootstrap that refuses unknown fields. It is
reassuring that Apollo reaches for the same separation elsewhere. It is not
evidence about Apollo's console, and no claim here rests on it.

Finally, this ground has been walked before. Issue #96, closed won't-do on
2026-08-17 and preserved in `docs/operations/APOLLO-WORK-QUEUE.md` "because the
reasoning is the record," asked whether the console tenant should become a
per-request fact. It was refused on three grounds that all still hold: Foundry
binds tenants by provisioned hostname with OAuth2 on every call, which Conduit
has no equivalent of; there is no `TenantID` anywhere in `internal/`; and
`GET /console/` is mounted with no auth wrapper, so a caller-selected tenant
would let an anonymous client choose what identity the console asserts. Any
proposal to make the bootstrap richer or more dynamic inherits all three
objections. This one does not, because it proposes neither.

### Migration, if a future v2 is ever justified

`schemaVersion` exists and both validators compare it by exact equality —
`bootstrap.SchemaVersion != BootstrapSchemaVersion` in
`internal/consoleui/bootstrap.go`, and `root.schemaVersion !==
BOOTSTRAP_SCHEMA_VERSION` in `web/src/runtime/bootstrap.ts` — and both reject
unknown *and* missing fields, Go via `DisallowUnknownFields` and TypeScript via
`exactKeys`. This is a rigid contract by construction, which is the point: it is
what makes the surface closed. But it means additive evolution is not available.
A v1 console cannot ignore a v2 field, and a v2 console cannot accept a v1
document.

For the two halves the Hub ships, that rigidity costs nothing. The console
bundle is built into the Go binary and the Hub renders the meta at handler
construction, so the parser and the injector are one artifact and always move
together. There is no version negotiation to do because there are no two
independently-deployed parties.

The third party is where the hazard is, and it is the part a migration plan has
to be about. The document itself is operator-authored: the Helm chart writes the
typed `consoleBootstrap` values into a ConfigMap
(`deploy/helm/platform-hub/templates/console-bootstrap.yaml`) mounted read-only
and read through `CONSOLE_BOOTSTRAP_FILE`, and per
`docs/ui/contracts/runtime-bootstrap.md` "a configured file that cannot be read
or validated fails Hub startup." That failure mode is correct — a Hub that
booted with unreadable branding would be a Hub serving invented branding — but
it means a Hub upgraded to a v2-only validator will refuse to start against an
operator's existing v1 ConfigMap. The upgrade would fail closed at exactly the
moment the operator is least able to diagnose it.

So if a v2 is ever justified, the migration is:

1. **Have a reason that survives the questions above.** Neither an API origin
   nor a capability set is such a reason, for the reasons given. A new
   presentational field might be.
2. **Accept both versions in the Hub for one release.** Decode v1 and v2 into
   the same internal `Bootstrap`, defaulting the new fields, so an existing
   ConfigMap keeps a Hub starting. Keep exact-equality dispatch on the version
   string; do not soften it into a prefix or range check, because "accepts two
   named versions" is still a closed surface and "accepts anything that looks
   like a version" is not.
3. **Emit only v2.** The Hub renders the meta, so the browser parser only ever
   needs to understand the version the Hub emits, and it can go straight to
   v2-only.
4. **Roll the operator's document.** The chart already hashes the document into
   the Pod template so an approved change rolls the Hub, which makes this an
   ordinary reviewed change rather than a special procedure.
5. **Drop v1 acceptance in the following release**, once no mounted document
   claims it.
6. **Extend the shared fixtures under `testdata/console-bootstrap` first**, not
   last, since they pass through both the Go and TypeScript validators and are
   the only place a divergence between the two would be caught.

Until such a reason exists, `schemaVersion` should keep the value it has. A
version bump with nothing behind it spends the migration budget and buys a
number.

## Recommendation

Leave `conduit.console.bootstrap/v1` exactly as it is. Its deliberate exclusion
of API-origin, authentication, capability, script, feature-toggle, and
security-policy fields is not a gap awaiting Apollo evidence; it is the correct
design for a document served to an anonymous caller and frozen at handler
construction, and the corroborating practice available — Foundry's pre-auth
hydration carrying four nullable presentational strings, with feature flags in
a separate opt-in file — agrees with it from another product's direction. The
capability seam belongs where it already is, at authenticated, per-request
`GET /v1/session`.

The one thing worth adding is not a field. It is this document, so that the next
person to ask "how does Apollo do hydration?" finds the searched-and-empty
result recorded with its sources, rather than repeating the search and being
tempted, as this exercise was, by a `me` query and a name collision.
