# Adopt persisted native Space groupings

Status: Accepted (owner decision in issue #17)
Supersedes: ADR 0007 group 2's continued refusal of five Space coordinates

## Evidence and decision

The adopted decision in #17 calls for first-class Space grouping, CLI Space
selection (including guest-user publication), and five GraphQL `spaceId`
coordinates. The Rosetta CLI notes v0.500.0/v0.531.0 corroborate Space selection.
The existing `apollo-identifier-grammar.json` derives ApolloSpaceId's origin
from @osdk/client.unstable 2.43.0, tarball SHA256
`a892d51988c83c9008aedf0e74ccf1ead8d5c808cb53c615b54d290e2817c02e`:
an externally issued Apollo Space identity arrives by Agent report. A Foundry
namespace identifier is a different concept. The recovered authorization
inventory contains no Apollo Space role family.

These sources do not establish a native Hub Space storage schema, identity
format, or membership API. The new model is explicitly CONDUIT_NATIVE and
registered as `spaces.NativeGrouping`. It does not issue an Apollo identity or
reinterpret a Foundry namespace. A sanitized Apollo Space record establishing
identity, grouping metadata, ownership and guest-user publication would
require reimplementation against that evidence.

## Native model and persistence

A Space has a random 128-bit `space_` identity, display metadata, authenticated
creation subject, UTC creation time and native provenance. Names are not
identities: duplicate names can describe different Spaces. Identity records
cannot be overwritten by a create retry.

A persisted anchor associates one Environment, Product or Bundle with one
Space. Entities inherit their Environment's grouping; Product Releases inherit
their Product's. Duplicate assignment to the same Space preserves the first
assignment and provenance. Assignment to another Space fails rather than
silently moving a tenant boundary. This immutability is a native first-version
policy, not a statement about Apollo membership editing. A binding is grouping
intent; it does not assert that the resource exists, is live or is deployed.

PostgreSQL owns a separate `spaces` migration component and tables, rather than
adding fields to the legacy delivery snapshot. An older Hub rewriting its
snapshot cannot erase these records. Resource keys are unique across Spaces,
and a foreign key requires the Space to exist. The development file store
installs complete private files atomically without replacing another writer's
record. Missing, corrupt and failed reads remain distinct outcomes.

## Authorization and implementation slices

Selecting a Space conveys no permission. Creation, assignment, Space visibility
and each resource read must use caller-bound authority; a visible Space cannot
expose a resource the caller cannot read. An authorization infrastructure
failure is not an empty roster. No Apollo Space roles are invented.

The first slice implements the domain, application ports, memory/file/PostgreSQL
stores and behavioral tests. The PostgreSQL gate exercises independent pools,
concurrent cross-Space claims, persistence through reopen and a legacy snapshot
write, and corruption checks. Application tests prove authority filtering and
failure propagation; file tests prove private persistence and concurrent
non-replacement.

The HTTP/CLI selection and publication integration, guest-user case, and five
GraphQL coordinates remain required by #17. This foundation does not yet expose
those paths or claim that #17 is complete. The refusal records are changed only
when their resolvers are backed by these persisted assignments and behaviorally
tested; declarations must not get ahead of their data sources.

## Native HTTP management slice

The native `/v1/spaces` adapter mounts only when composition supplies its store.
Creation and immutable assignment use the canonical `platform/root`
`permissions:manage` decision, retaining the original carrier for delegated
credentials. Space visibility requires that authority or a readable anchor;
rosters independently filter every resource using the same operation as its
existing Hub read surface. Bundle visibility follows the existing platform read
boundary. These are Conduit choices, registered as `spaces.HTTPSurface`.

The request body cannot supply identity, creation provenance, or a stronger
provenance tier. Unknown fields and trailing documents fail before persistence.
Identity-provider diagnostics are replaced with a generic transport error.
Composition tests exercise cross-tenant reads, filtered shared-Space rosters,
reader write refusal, assignment conflict, spoofed creation metadata, and
service-account delegation that must not inherit bootstrap administration.
This slice does not yet connect the production store constructor or CLI/GraphQL
selection; those remain part of #17.

The Hub executable opens this component in the same durability mode and data
root as its delivery repository (or the same mounted PostgreSQL DSN). Worker
mode does not initialize the HTTP-only component. PostgreSQL backup/restore
checks the Space migration ledger before constructors run, then compares the
exact Space identity and assignment, including their provenance, after restore.
This does not claim old-binary compatibility or complete CLI/GraphQL coverage.

## Native CLI management slice

`conduit space create NAME --description TEXT`, `space get SPACE_ID`, and
`space list` manage and inspect native records. `space bind SPACE_ID
ENVIRONMENT|PRODUCT|BUNDLE RESOURCE_ID` submits immutable grouping intent;
`space bindings SPACE_ID` returns only anchors independently readable by the
caller. These command paths are registered as `spaces.CLIManagement`, not
claimed as recovered Apollo CLI paths. JSON/YAML records retain native
provenance and the authenticated creator; the CLI cannot supply either.

The normal CLI connection factory and existing identity transport send requests
to the Hub. No local filtering substitutes for Hub authority. Transport errors,
including forbidden reads and conflicting assignment, fail the command. An
actual Hub round trip tests the commands, cross-tenant filtering, provenance,
and the absence of mutations after rejected commands. Global selection,
profile persistence and publication integration remain required follow-up work.

## GraphQL assignment slice

All five recovered `spaceId` fields now read persisted assignments through a
caller-bound port. Environment and Entity use the native Environment ID;
Product and Product Release use the owning Product ID; Bundle uses its stored
native ID. Creation of no assignment is not inferred: a configured store must
return not-found before the field answers null. Missing configuration, storage
failure and denied access remain errors and are not counted as served values.
Every field retains observed coordinate provenance and discloses its native
value tier. The choices are registered as `spaces.GraphQLGrouping`.

The Bundle entry point now reads the authorized native Bundle identity catalogue
under the existing `platform/root` read boundary. Its RID uses the established
`ri.conduit.main.bundle.<locator>` namespace, not a guessed Apollo type token.
The unusual recovered `bundles(rids: RID!)` spelling still has a single argument
and a single result; it is not silently converted to a batch API. The stored
Bundle record, rather than assignment intent alone, establishes its existence.

An actual Hub integration test creates different Space assignments for the
Environment, Product and Bundle and verifies all five fields, inheritance,
value disclosure, independent read authority and sanitized storage failures.
Namespace remains separate. This slice does not complete CLI selection or the
guest-user publication requirement of #17.

## CLI selection and publication slice

`--space-id` and `CONDUIT_SPACE_ID` select a native Space for Environment listing
and Product publication. `configure` persists the optional `spaceId` in the
versioned profile. Explicit flags override environment/profile values; an
explicit empty flag clears the profile selection. The Hub validates a readable
persisted Space and filters assignment before search/pagination. Every resource
still requires its existing canonical, carrier-aware read authority.

Selected Helm publication uses the native bundle route, preserving every input;
Terraform uses its existing native route. Existing artifact, Product-creation
where necessary, and Release-creation authorization runs before assignment.
The caller must also read the selected Space through an existing anchor.
There is no inferred primary organization: an external user with explicit grants
can select a shared Space. Space visibility grants no publication permission.
This transport and reservation policy is registered as
`spaces.SelectionPublication`, not claimed as private Apollo compatibility.

Before publication, the service reserves immutable Product grouping intent under
that caller's authority. This intent can survive a later publication failure and
does not assert a resource or artifact exists. A retry preserves the first
assignment and provenance; explicitly selecting another Space fails with a
conflict instead of moving the Product. Without an explicit selection, legacy
publication behavior remains and any existing assignment stays anchored.

Actual CLI-to-Hub tests span more than one selected catalogue page, independently
filter guest resource grants, publish Helm/Terraform metadata into a shared
Space, reject inaccessible-Space and read-only callers without recording an
assignment/publication, preserve conflicts, and reject delegated bootstrap
inheritance. Profile tests cover persistence, override/clear precedence and
invalid native identities. No deployment or registry publication is performed.

## Native console index selection slice

The native `spaceId` REST constraint narrows seven existing indexes:
Environments, Products, Plans, Fleet installations, change requests, promotions
and Bundles. Plans/installations/change requests use their explicit Environment;
releases/promotions use their Product; Bundles use only their own stored binding.
No membership is inferred from deployment observations, names or Bundle contents.
After validating visibility of the selected Space, trusted composition loads
private membership once for the request. Existing record-read decisions still
govern every row, including author/reviewer access to Plans and change requests
without a grant to read the whole Environment. Selection conveys no permission.
Search, counts, Fleet facets and pagination use the scoped inputs.

Scoped exact-id reads used for page expansion and linked selections enforce the
same constraint. The Space ID is an additional cursor filter; omitted selection
preserves the prior unscoped fingerprint. An unavailable or malformed remembered
selection fails visibly instead of widening to All Spaces. These are extensions
of `spaces.SelectionPublication`, not private Apollo protocol compatibility.

The channel-board REST endpoint also accepts explicit `spaceId`, using the same
validated Environment membership and independent per-row read authority. Invalid
raw query encoding is refused before selection or publication; malformed explicit
selection cannot become an unscoped read or metadata write. The console's global
selector does not yet forward its selection to the channel board.

This slice does not scope Packaging's Connect transport,
workspaces, upgrades, multi-Environment rollouts or mixed-resource audit. Their
relationships/transport and, for audit, SQL paging and projection proofs need
separate changes. Platform governance directories and provider configuration
remain global. The selector discloses these boundaries; #823 remains broader
than this slice.
