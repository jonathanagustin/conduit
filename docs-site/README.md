# Docs site

Published guides for this repository: <https://jonathanagustin.github.io/conduit/>.

The site is an Astro Starlight project. Pages are authored in `src/content/docs/`. They summarize the repository's own README, `docs/README.md`, architecture notes, and decisions, and they link those documents instead of copying them in full. `npm run build` copies the two console demo images from `docs/ui/` into `public/demo/` and writes `src/generated/versions.json` for the single current channel. Those build assets are gitignored. The generated native reference and readiness pages are committed for review.

There is one documentation channel, labeled Current. The header still has the version control and the version-scoped search used by the sibling documentation site, so a later channel can be added without changing the navigation chrome. Groups are Getting started, Concepts, Reference, and Project. The active group opens. The others stay collapsed.

`npm run build` needs Node 26, matching `mise.toml`, then runs the content checks under `scripts/`. `npm test` runs Playwright against `npm run preview`. Build first. The browser checks cover the landing page, theme, sidebar, search, the missing-page route, and the narrow viewports.

The Docs workflow builds on relevant pull requests and pushes. Pages upload and deployment run only after an explicit dispatch on `main`; a successful build does not publish. GitHub Pages must use the GitHub Actions source for that manual deploy job. Product publication remains separate and manual.

Native references are generated from the actual CLI constructor, the four native OpenAPI documents, the protobuf source files and the Hub route register. Run `go run ./tools/native-reference --write` from the repository root after reviewing those inputs. The generator supplies an empty configuration environment and never executes a CLI command, hook or service call. `go run ./tools/native-reference` checks for drift. The API inventory explicitly distinguishes operations with a native schema from the missing-contract backlog; it does not synthesize schemas from route names or copy the private reference corpus.

Readiness and the roadmap are generated from `docs/operations/readiness-evidence.json`. Review the merged baseline, source claims and live tracker states before updating that record, then run `node scripts/verify-readiness.mjs --write`. Normal offline checking verifies dates, commit distance, full-history availability, evidence paths and committed issue states. The weekly read-only workflow additionally asks GitHub for current issue states; a live read does not silently refresh the committed snapshot. With no authorized GitHub token, inability to read a private repository fails the live check. Neither freshness check is a complete test receipt.

The header wordmarks are `src/assets/conduit-wordmark-light.svg` and `src/assets/conduit-wordmark-dark.svg`. They use the console's three sapphire chevrons. `public/favicon.svg` is the console squircle mark.
