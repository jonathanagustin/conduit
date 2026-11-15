# Frontend toolchain

Conduit's `web/` package uses Vite+ 1.6.0 for the console build, preview, unit
runner, linting, and formatting. Syncpack 15.3.3 checks manifest/catalog policy.
Both are exact locked development dependencies. Installation uses the existing
Node 26.13.1 and pnpm 12.18.0 pins; a global `vp` install is unnecessary.

```sh
pnpm --dir web install --frozen-lockfile
pnpm --dir web check
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
pnpm --dir web dev
```

`dev` retains the custom Node assistant bridge and starts `vp dev` beside it.
Running `vp dev` directly starts only Vite, just as running `vite` directly did.
The `/console/` base, Hub bootstrap marker, font assets, output directory, proxy,
and Playwright tests are unchanged. The template's Next scripts remain separate.

## Bundle consistency

The pnpm catalog pins `vite-plus`, the aliased core, and Vitest. Overrides keep
plugins on that core and prevent a second Vitest version. The installed-bundle
check also verifies the actual physical runner/core instance and the declared
pnpm pin, before builds and unit tests. Its negative tests cover independent
core/runner updates, duplicate runner resolution, and package-manager drift.

Vite+ 1.6.0 bundles Vite 8.3.7, Vitest 5.1.0, Oxlint 1.93.0, and Oxfmt 0.78.0.
The core package's own version is 1.6.0; its one-version peer-alias exception is
limited to that exact distribution. The migration from standalone Vite 8.3.8
therefore uses one older Vite patch. The
[8.3.8 release notes](https://github.com/vitejs/vite/releases/tag/v8.3.8) list bug
and performance changes; the embedded build, native bridge fixture, unit tests,
and browser checks validate Conduit's use. This does not claim every upstream
Vite feature is equivalent between those patches.

Renovate proposes the parent Vite+ update. Its core/Vitest catalog pins must be
updated together from that release's bundle metadata; independent updates are
disabled. For an upgrade, use the pinned target CLI and review every diff:

```sh
# Run from web/, replacing TARGET with a reviewed, age-qualified exact version.
pnpm dlx --package=vite-plus@TARGET vp migrate \
  --no-interactive --no-agent --no-editor --no-hooks
```

Keep the pnpm pin, release-age quarantine, bounded peer rule, and project scripts
when reviewing generated configuration. Preserve unrelated resolved application
versions. Then run the installed-bundle check and the complete local CI suite.
The current wiring installs no new Git hooks, editor setup, or agent files.

## Dependency and source checks

`pnpm --dir web check:deps` runs Syncpack without changing files. Its catalog
policy requires the three Vite+ tool references to use `catalog:` and exact
catalog versions; Syncpack itself is pinned. Release-age settings continue to
come from `pnpm-workspace.yaml`. `fix:deps` is an explicit maintainer command,
not a CI action. The former `@assistant-ui/next` `latest` specifier is pinned to
its already validated locked version, 0.0.25; it was not upgraded.

`check` combines Syncpack, bundle checks, formatting, and type-aware linting.
`test` retains the separate TypeScript check and every unit/browser test. The
migration baseline has zero lint/type errors and 202 visible type-aware lint
warnings; this change does not suppress those warnings.

The 80-column formatter settings moved into `vite.config.ts`. Authored files
were formatted to that existing policy. Generated ontology metadata and the
integrity-pinned basemap are excluded from rewriting: the generator's `--check`
and the basemap's SHA-256/unit test continue to verify their exact bytes. Proto
regeneration uses `vp fmt` with the same Oxfmt version.

Upstream references: [Vite+ migration](https://viteplus.dev/guide/migrate),
[migration rules](https://viteplus.dev/guide/migrate-rules), and
[Syncpack catalogs](https://syncpack.dev/version-groups/catalog/). These are public
tool contracts; Conduit's bridge, verification wiring, and dependency policy are
Conduit extensions, with no private upstream UI/protocol claim.
