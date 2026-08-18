# Foundry's frontend architecture, and what Conduit should take from it

This note answers a single question: the Conduit console should be "set up
similar to how Foundry is set up" -- what, precisely, does that mean, and which
parts of it are worth having in a product with **one** console whose build
output is embedded in a Go binary.

Two things about the source it reads from, stated once so nothing below has to
restate them:

- Every claim here is grounded in a file path inside the local `foundry`
  checkout. Paths written bare (`packages/dll/rspack.dll.js`) are relative to
  that checkout's root; Conduit paths are written with a leading `web/`,
  `scripts/`, `internal/`, or `docs/`.
- That checkout is a **reconstruction** of Palantir Foundry's frontend, not
  Palantir's source. Where it makes a claim about the real Foundry runtime it
  cites its own recovered evidence, and this note repeats such claims as
  *"the reconstruction records X, citing Y"* rather than as fact. Under
  `docs/operations/TENANT-FIDELITY.md` the whole of Part 1 is corroborating
  evidence at best. See "What could not be determined" for the specific
  boundaries.

---

## Part 1 -- the map

### 1.1 Workspace layout

`pnpm-workspace.yaml` admits 31 packages across four roles. There are no globs;
every member is written out by hand.

| Role | Path | Count | What a member is |
| --- | --- | --- | --- |
| Deployable host | `apps/*` | 7 | A bundler config, an `index.html`, a runtime-config reader, an entry `.tsx`, and E2E specs. Almost no product code. |
| Portable application | `packages/foundry-apps/*` | 13 | The application itself as a library: domain, ports, adapters, renderer. No bundler, no HTML. |
| Shared component | `packages/components/*` | 9 | Cross-application presentation primitives. |
| Shared infrastructure | `packages/foundry-ui`, `packages/workspace-app`, `packages/conjure-clients`, `packages/osdk`, `packages/dll` | 5 | UI vocabulary, the workspace shell, transport clients, the SDK composition, the shared vendor bundles. |

The `apps/` vs `packages/foundry-apps/` split is the load-bearing one, and it is
a **host/library** split, not a duplication. Compare the two Compass manifests:

- `packages/foundry-apps/compass/package.json` -- name `@foundry/foundry-app-compass`,
  `"type": "module"`, an `exports` map, a `typecheck` script, a `test` script
  (`tsx --test`), and **no** `build`. Its dependencies are `@foundry/foundry-ui`
  and a vendored API package; React and Blueprint are `peerDependencies`.
- `apps/compass/package.json` -- name `compass`, scripts `dev`/`build` that are
  `rspack serve`/`rspack build`, `devDependencies` on `@rspack/*`,
  `css-loader`, `style-loader`, and a dependency on
  `@foundry/foundry-app-compass: "workspace:*"`.

`packages/foundry-apps/compass/README.md` states the intent directly: "Ports and
Foundry transport adapters are kept outside the React renderer so the default app
model can render synchronously and tenant data can hydrate it without coupling
the view to a specific tenant", and it names the directory contract --
`src/compass-app/` renderer, `src/domain/` tenant-neutral contracts,
`src/adapters/` transport, `src/config/` + `schemas/` validated configuration.
The adapter files on disk match it (`FoundryCompassTrashAdapter.ts`,
`InMemoryProjectsFilesAdapter.ts`, and a `.test.ts` beside each).

`docs/REPOSITORY-LAYOUT.md` gives the rule in one line per directory: "`apps/`
-- The seven runnable frontend hosts used by the full source preview";
"`packages/` -- Reusable TypeScript packages and the shared workspace shell".

The split is **enforced, not documented**. `tooling/frontend/scripts/gate-package-admission.mjs`
fails when a workspace entry contains a glob character, when a directory under
`packages/components` or `packages/foundry-apps` has a `package.json` but is not
listed in `pnpm-workspace.yaml`, when a component package has no `src/`, and when
an app manifest carries `provenance.source == "live-launcher-registry"` (an
application known only from a registry listing may not masquerade as a built
host). `tooling/frontend/scripts/build-each.mjs` refuses to run if any admitted
`apps/*` member lacks a `build` script.

Two smaller conventions worth naming because Conduit already borrowed one:

- **Every package manifest carries a `provenance` object.** It records what kind
  of claim the package makes and what would displace it -- e.g.
  `packages/conjure-clients/package.json` says
  `"kind": "incomplete-reconstruction-workbench"`, `"admittedReusableCode": false`,
  and gives a reason. `web/package.json` in Conduit already has the same field.
- **Package version is release identity, not semver.** The `packages/components/*`
  members are all `6.521.55`; `apps/*` carry `6.531.0-rc14`, `6.531.38`,
  `6.532.0`, `6.524.73`. `packages/foundry-apps/compass/README.md` says
  explicitly: "The package version is pinned to the recovered batch version,
  6.521.55. This is release identity, not a claim that the local filenames are
  vendor-authored names."

### 1.2 How the shell composes many applications

**It is not Module Federation.** The reconstruction says so in the file that
defines the mechanism. `packages/workspace-app/server/foundry-entry-manifest.ts`
records, as recovered hypothesis H15:

> the ordered load graph is the app entry followed by 10 shared provider DLLs
> (no Module Federation -- a custom Rspack JSONP app + shell-ordered global DLLs).

and as H16, that "those DLLs are byte-identical SINGLETONS across apps (same
content-hash) -- loaded once, reused".

The mechanism has three parts.

**(a) A server-rendered entry manifest.** The same file describes the app's
`index.html` as "an unrendered Mustache template
(`{{scripts}}`/`{{styles}}`/`{{entryManifests}}`)" that "the workspace shell
renders per request, filling in the ordered script/style list". It types the
payload: `EntryManifest` with `scripts`/`styles`/`linkedScripts`/`linkedStyles`,
each an `AssetRef` that resolves two ways -- a `casUrl`
(`/assets/content-addressable-storage/frontend/<hash>.<ext>`) and a `staticUrl`
(`/assets/static/<batch>/<version>/<track>/asset/<name>/<hash>.<ext>`).
`ApplicationEntryManifest` wraps per-app (`app[]`) and per-subroute
(`subroutes[]`) manifests. The schema version is pinned:
`ENTRY_MANIFEST_SCHEMA_VERSION = "3.0.0"`.

`apps/compass/src/index.html` is that template -- its `<head>` is four comment
markers, `<!--#ENTRY_META#-->`, `<!--#ENTRY_PRELOADS#-->`, `<!--#ENTRY_STYLES#-->`,
`<!--#ENTRY_SCRIPTS#-->`, and nothing else. `apps/compass/rspack.config.js`
installs `FoundryEntryManifestPlugin` (from
`tooling/frontend/buildkit/foundry-entry-manifest-plugin.cjs`) to fill them,
parameterised by `entryName`, `assetName`, `batch`, `version`, `track`.

**(b) Shared vendor DLLs, not per-app vendor bundles.** `packages/dll/rspack.dll.js`
builds `foundryReactDll`, `foundryBlueprintIconsDll`, `foundryGraphqlDll`, and
`foundryBlueprintCoreDll`, each with `library: { type: "var", name }` so it
publishes a `foundry<Name>Dll` global, plus a `DllPlugin` manifest.
`packages/dll/dllReferences.js` exports `dllReferencePlugins()`, which each app
spreads into its rspack `plugins` (`apps/compass/rspack.config.js` line 1 of the
`plugins` array) so React, Blueprint and graphql resolve to those globals rather
than being re-bundled.

The build is deliberately sequential -- `react` -> `{blueprint-icons, graphql}`
-> `blueprint-core` -- and the file explains why at length: a DLL entry pulls its
whole dependency graph, so `@blueprintjs/core` would bundle its own private React,
and at runtime "Blueprint calls a hook against its bundled React whose dispatcher
is null, and you get the dual-React crash". `vendorAlias()` in
`dllReferences.js` pins producer and consumers to the same resolved vendor
directories, because "under pnpm, a lib with peer deps resolves to a different
`.pnpm` virtual path per consumer" and the manifest identities would otherwise
not match.

The same file records a failure mode worth carrying as a lesson even where the
mechanism does not transfer: a DLL built in production mode under a development
app gives every component `jsxDEV is not a function`, and "a stale production DLL
on disk broke 23 of 34 E2E slices this way while every static gate stayed green".
The fix was a `.dll-build-mode` stamp file, so the consumer can *detect* the
mismatch instead of trusting the file's existence.

**(c) The in-repo shell is a plain React composition root, and it is small.**
Two things are called "shell" and they are not the same:

- `packages/components/shell-application` is **layout primitives only**.
  `ShellApplication.tsx` is a five-line component returning a `<div>` with a CSS
  module class. Its manifest describes it as "Tenant-neutral Foundry shell layout
  primitives shared by Foundry and Gotham applications." It composes nothing.
- `packages/workspace-app/src/WorkspaceApp.tsx` is the actual composition root,
  and it is 111 lines. It holds a `BODY: Record<string, ReactNode>` map from a
  surface id to an imported module (`files`, `objects`, `ontology`, `releases`,
  `compass`, `control-panel`, `quiver`, `contour`), a `useState` for the active
  view, a Blueprint `Navbar`, and a sidebar driven by `ALL_APPS`/`PLATFORM_APPS`
  from `@foundry/application-portal-dialog`. Its own comment: "Mirrors Foundry's
  composition root -- the shell owns navigation; feature packages own their
  surfaces."

So there are two composition models in the same repository: a **runtime**,
manifest-driven, multi-bundle one that the reconstruction attributes to the real
Foundry, and a **build-time**, single-bundle, static-import one that the
reconstruction itself runs on. The second is the one a single-console product
would recognise.

**(d) Runtime configuration arrives as an HTML meta tag.**
`apps/compass/src/runtime-config.ts` reads
`<meta name="x-workspace-page-environment-data" content="{...JSON...}">` -- its
comment stresses it is "an `x-`prefixed name but delivered as an HTML META TAG,
NOT an HTTP header" -- and maps exactly two keys out of it
(`graphqlGatewayApi` -> `graphqlEndpoint`, `compassApi` -> `apiBaseUrl`), with the
rest deliberately unconsumed. A `declare global` block types
`__COMPASS_CONFIG__` as deployment-owned hydration, and notes "The browser bundle
contains no tenant identity or credentials."

Conduit already implements this pattern independently:
`web/vite.config.ts` injects `<meta name="x-conduit-console-bootstrap">` in dev
only, `web/src/runtime/bootstrap.ts` reads it, and `internal/consoleui/handler.go`
renders it in production. This is convergent, not copied -- and Conduit's version
is the tighter one, because the Go side validates the payload
(`internal/consoleui/bootstrap.go`) before it is served.

### 1.3 Typed API clients

There are two client packages and they are **not** the same kind of thing.

**`packages/osdk` is real, and it is a composition, not a generator.** Its
manifest depends on Palantir's published `@osdk/*` packages -- `@osdk/client`,
`@osdk/react`, `@osdk/foundry.admin`, `@osdk/foundry.core`,
`@osdk/foundry.datasets`, `@osdk/foundry.filesystem`, `@osdk/foundry.ontologies`,
`@osdk/faux`, `@osdk/cbac-components`, `@osdk/react-components`. Its
`provenance.kind` is `"public-upstream-package-composition"`. The 68-line
`src/index.ts` re-exports `createClient`, adds a `createFoundryPlatformClient`
helper, and re-exports the `@osdk/react` hooks. Its own comment names the
contract those upstream packages are generated from: the `@osdk/foundry.*`
namespaces are "auto-generated from Foundry's API spec; the machine-readable
contract is `@osdk/docs-spec-platform`'s `PLATFORM_API_IR`". So: generated from a
pinned contract, **upstream**, then consumed here as versioned npm dependencies.
Node-only surface is fenced off behind a separate `./faux` export
"so importing `@foundry/osdk` cannot pull `node:crypto` or `node:util` into
reconstructed frontend bundles."

**`packages/conjure-clients` is not generated at all.** Four hand-written files,
481 lines total (`compass.ts`, `ontology.ts`, `multipass.ts`, `index.ts`). Each
exports a `create<X>Service` interface with a **mock** and an **HTTP**
implementation behind the same type -- `createMockOntologyService` /
`createHttpOntologyService`. The manifest's `codegen` script is literally
`echo 'TODO: conjure-typescript generate from ../../../servers/**/*-api (replaces the mock)'`
and its `provenance` says `"kind": "incomplete-reconstruction-workbench"`,
`"admittedReusableCode": false`, reason: "code generation from recovered Conjure
contracts is still TODO".

The transferable idea here is the *shape*, not the tooling: one interface, two
implementations (in-memory and over the wire), so the renderer never learns which
it has. That is the same hexagonal boundary `packages/foundry-apps/compass/src/adapters/`
uses, and it is what makes `CompassApp.render.test.tsx` a synchronous unit test.

### 1.4 Build, dev, test, lint

| Concern | Foundry | Evidence |
| --- | --- | --- |
| Package manager | pnpm 10.32.1, pinned via `packageManager` and `corepack` | root `package.json`; `Taskfile.yml` `deps` task |
| Node | `>= 24` | root `package.json` `engines` |
| Bundler | Rspack 1.4.11 (pinned by `pnpm.overrides`, not just a range) | root `package.json`; `apps/*/rspack.config.js` |
| Build orchestration | a hand-written script, not Turbo/Nx | `tooling/frontend/scripts/build-each.mjs`, invoked as `pnpm build:each` |
| Typechecking | `tsgo` (`@typescript/native-preview`) per package | `packages/*/package.json` `"typecheck": "tsgo --noEmit -p tsconfig.json"` |
| Unit tests | **`node --test`**, driven by `tsx`, with a CSS stub loader | e.g. `packages/foundry-apps/compass/package.json`: `tsx --import ../../../tooling/frontend/scripts/css-stub-loader.mjs --test "src/**/*.test.ts*"` |
| Gate self-tests | `node --test tooling/frontend/buildkit/*.test.mjs` | root `package.json` `test:buildkit` |
| Browser tests | Playwright 1.62.0, **many** configs | root `playwright.config.mjs` plus 20 per-app configs found under `apps/*/e2e/` |
| E2E style | standalone `.e2e.mjs` scripts run by `node`, not by Playwright's runner | root `package.json`: `e2e:differential:compass`, `e2e:slice:control-panel`, `e2e:journey:ontology-modification`, ... |
| Lint / format | **none** | no `eslint.config.*`, `.eslintrc*`, `biome.json`, `.prettierrc*`, or `.editorconfig` anywhere in the tree |
| CI | one workflow, two jobs (`rust`, `required`); the frontend job is a single `bash scripts/verify-repository.sh` | `.github/workflows/trunk.yml` |

Two observations that matter for the plan.

The root `playwright.config.mjs` sets `testDir: "./tests"` -- and **there is no
`tests/` directory at the repository root.** The root config is vestigial; the
work happens in per-app configs. This is what an unpoliced multi-config setup
decays into.

The absence of any linter or formatter is not an oversight to imitate. It is the
single clearest respect in which Foundry's frontend is *behind* Conduit's Go side,
which runs `gofmt`, `staticcheck`, and `govulncheck` as named CI gates
(`scripts/verify-local.sh`).

What Foundry does instead of a linter is **gate scripts**: `scripts/verify-layout.mjs`
(forbidden paths, canonical paths present, no duplicate workspace names, root
scripts point at files that exist), `scripts/verify-local-imports.mjs` (every
relative import resolves on disk, with `.js` -> `.ts`/`.tsx` mapping),
`tooling/frontend/scripts/gate-package-admission.mjs`, and
`tooling/frontend/buildkit/gate-surface.mjs`. That is the same instinct behind
Conduit's `scripts/verify-domain-boundaries.mjs` and
`scripts/verify-console-contract.mjs`, and it is the part of Foundry's method
that transfers cleanly.

### 1.5 Versioning and dependency management

- **Versions are not semver and do not float.** `packages/components/*` are all
  `6.521.55`; `packages/foundry-apps/*` range `6.521.55` -> `6.531.9`; `apps/*`
  range `6.521.60` -> `6.532.0`. `packages/foundry-ui` is `0.0.0` and
  `packages/osdk` is `0.1.0`, which is how you can tell which packages claim a
  recovered release identity and which do not.
- **Internal links are `workspace:*`.** e.g. `apps/compass` depends on
  `@foundry/foundry-app-compass: "workspace:*"`. Vendored first-party bytes use
  `link:` (`@palantir/compass-api: "link:../../vendor/npm/@palantir/compass-api"`).
- **React and Blueprint are exact-pinned everywhere and appear as
  `peerDependencies` in libraries**: `react: "18.2.0"`,
  `@blueprintjs/core: "5.19.1"`. `packages/components/shell-application` is the
  exception that proves the rule -- it widens to
  `"@blueprintjs/core": "^5.19.1 || ^6.17.2"` and `"react": "^18.2.0 || ^19.0.0"`
  because it is shared with a second product.
- **Bundler versions are overridden, not merely ranged**: root `package.json`
  `pnpm.overrides` forces `@rspack/cli`, `@rspack/core`, `@rspack/dev-server` to
  exact versions even though the per-app manifests ask for `^1.4.0`.
- **Renovate** (`renovate.json`) groups non-major npm updates into one PR, groups
  non-major cargo updates into another, pins GitHub Action digests, requires
  `minimumReleaseAge: 3 days`, and puts every **major** update behind
  `dependencyDashboardApproval`. Conduit's `renovate.json` is the same family of
  configuration.

### 1.6 What could not be determined

Stated plainly, because guessing here is how a reconstruction gets cited as a
primary source.

- **Whether real Foundry's frontend is laid out this way.** The
  `apps/` / `packages/foundry-apps/` / `packages/components/` split is described
  in `docs/REPOSITORY-LAYOUT.md` as "a focused Foundry system, not a nested copy
  of the Palantir monolith" -- that is, an *organising decision of the
  reconstruction*. Nothing in the checkout claims the vendor's repository has
  these directories. Any statement that "Foundry splits apps from packages" is a
  statement about this checkout only.
- **Whether real Foundry uses Rspack.** The reconstruction's apps do. The entry
  manifest file infers "a custom Rspack JSONP app" from a recovered load graph
  (H15) rather than from a build configuration, and the JSONP chunk-loading shape
  is also what webpack emits. `packages/dll/rspack.dll.js` is the
  reconstruction's own build, so it is evidence of the reconstruction's choice,
  not the vendor's.
- **The ten provider DLLs.** `FOUNDRY_PROVIDER_DLL_ORDER` lists ten
  (`react-dll`, `blueprint-icons-dll`, `blueprint-core-dll`, `security-context-dll`,
  `graphql-dll`, `intl-messageformat-dll`, `monaco-editor-dll`, `pdfjs-dll`,
  `polyfills-dll`, `request-tracing-override-dll`). `packages/dll/rspack.dll.js`
  builds **four**. Whether the other six exist in the vendor's runtime, or are
  names read off a recovered manifest, is not resolvable from this checkout.
- **Whether Conjure clients are generated in real Foundry.** In this checkout they
  are not -- `packages/conjure-clients` is hand-written with a `TODO` codegen
  script. The presence of `api/conjure/foundry-apollo.conjure.json` in *Conduit*
  says Conjure IR exists as an artifact; it says nothing about Foundry's frontend
  build.
- **Which Blueprint major the vendor ships.** The DLL build defines
  `BLUEPRINT_NAMESPACE = "bp6"` while depending on `@blueprintjs/core: "5.19.1"`,
  citing a capture that observed `bp6` DOM. That is a namespace observation, not a
  version determination, and the two disagree.
- **How large the real thing is.** Seven `apps/` and thirteen
  `packages/foundry-apps/` is the reconstruction's admitted set. Foundry ships
  more applications than that; the checkout's own `AGENTS.md` says app names are
  codenames and warns that "searching the product name and finding nothing is not
  evidence of absence."
- **Runtime behaviour.** Nothing here was executed. No build was run, no server
  started, no bundle inspected. Every claim above is read off source and
  configuration.

---

## Part 2 -- a staged adoption plan for Conduit

### The three constraints that decide everything

1. **Conduit has one console.** `web/` is a single Vite app; `web/README.md`
   says "Nothing here resolves a workspace sibling."
2. **The build output is committed and embedded.** `web/vite.config.ts` writes to
   `../internal/consoleui/dist`; `internal/consoleui/handler.go` has
   `//go:embed dist`; `scripts/verify-console-assets.sh` rebuilds into a temp
   directory and `diff --recursive`s it against the committed tree. Any change
   under `web/src` is a change to a committed binary artifact.
3. **`internal/controlplane` may not grow**, and it is gated
   (`node scripts/report-migration-progress.mjs --gate` in
   `scripts/verify-local.sh`). Nothing in this plan may push work into it.

### The honest conclusion first

**Most of Foundry's frontend architecture is a solution to a problem Conduit does
not have.** Foundry's workspace split, its DLL singletons, its per-request entry
manifest, its `build-each.mjs` sequencing, and its 31-member pnpm workspace all
exist to make *many independently released applications* share *one* React and
*one* Blueprint at runtime. Conduit ships one bundle. Adopting the mechanism
would add a second bundler, a manifest format, a sequenced build, and a class of
failure whose own author documented it breaking "23 of 34 E2E slices while every
static gate stayed green" -- and would buy exactly nothing, because there is no
second consumer to share with. Reproducing it here would be cargo-culting, and
this document declines to recommend it.

What does transfer is Foundry's **method**: make the architecture machine-checked
rather than conventional, keep the renderer separable from the transport, and
keep the host thin. Conduit already does the first for Go
(`scripts/verify-domain-boundaries.mjs`) and partly for the console
(`scripts/verify-console-contract.mjs`). The stages below extend that reach.

Stages are ordered by value per unit of risk, highest first.

---

#### Stage 1 -- gate the console's internal architecture (implemented in this change)

**What changes.** A new gate, `scripts/verify-console-boundaries.mjs`, with a
planted-violation self-test, `scripts/verify-console-boundaries.test.mjs`, wired
into the `Operator console` lane of `.github/workflows/platform-ci.yml` and into
`scripts/verify-local.sh` under the same step name. It pins four invariants that
hold on the tree today:

1. **Layering.** `web/src` has five layers -- `runtime` (0), `api` (1),
   `components` (2), `features` (3), `app` (4), with `main.tsx` above them. A
   module may import from its own layer or a lower one, never a higher one. This
   is the rule Foundry states in prose in `packages/workspace-app/src/WorkspaceApp.tsx`
   ("the shell owns navigation; feature packages own their surfaces") and checks
   at package granularity in `tooling/frontend/scripts/gate-package-admission.mjs`.
2. **One network seam.** A direct `fetch(`, `XMLHttpRequest`, `EventSource`, or
   `navigator.sendBeacon` may appear only under `web/src/api/`. Today the only two
   are in `web/src/api/client.ts`. This is the console-side statement of
   `web/README.md`'s "Server state lives in the query cache", and of Foundry's
   ports/adapters boundary in `packages/foundry-apps/compass/src/adapters/`.
3. **Every relative import resolves on disk**, with `.js` -> `.ts`/`.tsx`
   mapping. Directly ported from `scripts/verify-local-imports.mjs`. The console
   writes `.js` specifiers for `.tsx` files throughout, so a rename that
   typechecks under `moduleResolution: "bundler"` can still leave a dead
   specifier behind.
4. **The `/console/` prefix agrees in all three places it is written.**
   `web/vite.config.ts` (`base`), `web/src/app/providers.tsx` (`basename`), and
   `internal/consoleui/handler.go` (`strings.HasPrefix(requestPath, ...)`).
   `web/README.md` already calls this "load-bearing" -- "with the default base the
   app works under `vite dev` and 404s on every asset once embedded" -- and
   nothing checked it. This is the pattern of
   `scripts/verify-console-contract.mjs`: read the Go source, compare it to the
   TypeScript, fail on disagreement.

**What it buys.** The console's layering becomes a property of the repository
rather than of whoever last reviewed a diff. Rules 1 and 2 are the parts of
Foundry's package split that are actually valuable at Conduit's scale -- they are
what would make a future extraction into packages mechanical instead of
archaeological. Rule 4 closes a failure that ships silently: the assets 404 only
once embedded, which is after every console test has passed.

**What it costs.** One gate script and one self-test. Roughly 60ms of CI. No
production code changes, no dependency changes.

**What could break.** The gate could be wrong about a rule and block a legitimate
change. Mitigated by keeping the rules to ones that hold on the current tree, and
by the self-test proving each rule both accepts the real tree and rejects a
planted violation of that specific rule.

**How it is verified.** `node scripts/verify-console-boundaries.test.mjs` (positive
fixture plus one planted violation per rule), `node scripts/verify-console-boundaries.mjs`
against the real tree, then the full `./scripts/verify-local.sh`, whose drift
check fails if the new CI step is not covered locally.

Note what stage 1 deliberately does **not** include: a rule forbidding
cross-feature imports. Three exist today -- `web/src/features/products/ProductsPage.tsx`
imports from `../promotions/`, and `web/src/features/environments/EnvironmentDetailPage.tsx`
imports from `../fleet/` and `../plans/`. Adding that rule would require moving
shared tags and graphs into `web/src/components/`, which means editing `web/src`,
which means regenerating and re-committing the embedded bundle. That is stage 2's
job, not a gate's.

---

#### Stage 2 -- lift genuinely shared feature code into `components/`, then tighten the gate

**What changes.** Move the three cross-feature imports' targets --
`statusTags.tsx` from `features/fleet`, `PlanStatusTag` from `features/plans`,
`ReleaseChannelGraph`/`TransitionDetail` from `features/promotions` -- into
`web/src/components/`, then add rule 5 to the stage 1 gate: a feature may not
import another feature.

**What it buys.** A real answer to "where does shared presentation live", which
is the question Foundry answers with `packages/components/*`. It also makes the
status-tag tables reachable from one place, which matters because
`scripts/verify-console-contract.mjs` already reads
`web/src/features/fleet/statusTags.tsx` by path.

**What it costs.** It edits `web/src`, so it requires `pnpm --dir web build` and
a committed regeneration of `internal/consoleui/dist`. The contract gate's file
paths must move with the file, in the same commit.

**What could break.** `scripts/verify-console-contract.mjs` hard-codes
`web/src/features/fleet/statusTags.tsx` and `web/src/features/plans/PlansPage.tsx`;
either must be updated or the move must stop short of them. The embedded bundle
diff will be large and must be reviewed as a build artifact, not as source.

**How it is verified.** `./scripts/test-console.sh` (which runs
`verify-console-assets.sh` and so proves the committed bundle matches a clean
build), `node scripts/verify-console-contract.mjs`, and the extended
boundaries gate.

---

#### Stage 3 -- an in-memory adapter behind each API module, so features test without a browser

**What changes.** Give `web/src/api/*.ts` the shape
`packages/conjure-clients/src/ontology.ts` uses: one exported interface, a
`createHttp<X>` and a `createInMemory<X>`, selected once at the composition root.
Feature-level logic then gets `node --test`-style unit tests instead of only
Playwright specs.

**What it buys.** The console's test pyramid currently has two layers --
`web/src/test/unit/` (two files) and `web/src/test/e2e/` (fourteen specs) -- and
almost everything lives in the browser layer. Foundry's
`packages/foundry-apps/compass` gets synchronous render tests precisely because
its adapters are swappable (`InMemoryProjectsFilesAdapter.ts` beside
`FoundryProjectsFilesAdapter.ts`). Faster feedback, and E2E specs freed to test
what only a browser can.

**What it costs.** A real refactor of `web/src/api` and its callers, a bundle
regeneration, and a new seam that must not become a second source of truth about
the Hub's JSON. Note that Foundry has not finished this itself: its Conjure
`codegen` script is still a `TODO` echo.

**What could break.** Fixtures drifting from the Hub. The mitigation already
exists and must be extended, not replaced: `scripts/verify-console-contract.mjs`
compares console vocabularies against the Go source, and the in-memory adapter
must be inside that comparison rather than beside it.

**How it is verified.** New unit tests, `./scripts/test-console.sh`,
`node scripts/verify-console-contract.mjs`, plus `go test ./internal/consoleui/...`.

---

#### Stage 4 -- generate the console's transport types from the pinned contract

**What changes.** `web/src/api/types.ts` is 93 hand-written lines whose own header
says they "follow the Go structs in `internal/controlplane`, field for field".
Where a coordinate is covered by `api/apollo-public.codegen.yaml` (which already
generates `internal/apollo/openapi.gen.go` via `oapi-codegen`), emit the
TypeScript from the same snapshot and check the generated file in.

**What it buys.** The `@osdk/foundry.*` property: types generated once from a
pinned contract and consumed as an artifact. It removes a class of drift the
console-contract gate cannot see, because that gate compares *value vocabularies*,
not *field sets*.

**What it costs.** A generator in the build, a committed generated file, and a
freshness gate. Real risk of scope confusion: the `/v1` control-plane surface that
`web/src/api/client.ts` talks to is **not** the Apollo public OpenAPI surface, so
this only applies where the two genuinely coincide. That overlap must be
established before any generator is written -- and establishing it is the first
task of the stage, not a precondition assumed away.

**What could break.** Generating types for the wrong surface would produce a
console typed against an API it does not call, which typechecks perfectly.

**How it is verified.** A generated-file freshness gate in the idiom of
`scripts/verify-console-assets.sh` -- regenerate into a temp directory, `diff`
against the committed file -- plus the existing console suite.

---

#### Stage 5 (recommended: do not do) -- split `web/` into a pnpm workspace

**What changes.** `web/` becomes a workspace with `web/packages/console-ui`,
`web/packages/console-api`, and `web/apps/console`, mirroring Foundry's
`packages/components` / `packages/foundry-apps` / `apps` split.

**What it buys.** At one console: nothing that stage 1's gate does not already
buy. The gate enforces the same boundaries the package split would, without a
lockfile per package, a build orchestrator, or a version policy.

**What it costs.** A second lockfile or a workspace-aware one, an orchestrated
build in front of `vite build`, per-package `tsconfig`s, and a
`gate-package-admission.mjs` of Conduit's own to stop the workspace from rotting
-- Foundry needed one, and still ended up with a root `playwright.config.mjs`
pointing at a `tests/` directory that does not exist.

**What could break.** `scripts/test-console.sh` and `scripts/verify-console-assets.sh`
both assume `pnpm --dir web <script>`; both would need rewriting, and both are the
gates that protect the embedded bundle.

**Verdict.** Revisit only when a second consumer of the console's components
actually exists -- a second embedded UI, or an extracted design system with a
release of its own. Until then this is the cargo-cult stage, and it is listed so
that the decision to skip it is recorded rather than forgotten.

### Deliberate divergences from Foundry, and why

| Foundry does | Conduit should not | Because |
| --- | --- | --- |
| Rspack + shared vendor DLLs (`packages/dll/rspack.dll.js`) | Keep Vite, one bundle | One console. Nothing to share a React with. |
| Per-request entry manifest (`packages/workspace-app/server/foundry-entry-manifest.ts`) | Keep the embedded, content-hashed `dist` | `internal/consoleui/handler.go` serves immutable assets from a Go binary; there is no asset server to render per request. |
| Multi-package pnpm workspace (`pnpm-workspace.yaml`, 31 members) | Keep a single `web/` package | See stage 5. |
| Release-identity versions (`6.521.55`) | Keep `web/package.json` at `0.0.0` | Conduit's console has no independent release identity; `VERSION` at the repository root is the product's. |
| No linter or formatter at all | Keep and extend the gate habit | Foundry substitutes gates for lint; Conduit already has both idioms on the Go side and should not regress the console to neither. |
| Hand-written Conjure clients with a `TODO` codegen script | Generate where a pinned contract exists (stage 4) | `api/apollo-public.codegen.yaml` already proves Conduit can do the thing Foundry deferred. |
