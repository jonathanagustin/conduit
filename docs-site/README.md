# Docs site

Published guides for this repository: <https://jonathanagustin.github.io/conduit/>.

The site is an Astro Starlight project. Pages are authored in `src/content/docs/`. They summarize the repository's own README, `docs/README.md`, architecture notes, and decisions, and they link those documents instead of copying them in full. `npm run build` copies the two console demo images from `docs/ui/` into `public/demo/` and writes `src/generated/versions.json` for the single current channel. Generated output is gitignored.

There is one documentation channel, labeled Current. The header still has the version control and the version-scoped search used by the sibling documentation site, so a later channel can be added without changing the navigation chrome. Groups are Getting started, Concepts, Reference, and Project. The active group opens. The others stay collapsed.

`npm run build` needs Node 26, matching `mise.toml`, then runs the content checks under `scripts/`. `npm test` runs Playwright against `npm run preview`. Build first. The browser checks cover the landing page, theme, sidebar, search, the missing-page route, and the narrow viewports.

The Docs workflow builds on pull requests that touch this directory, the README link, or the workflow file. A push to `main` also uploads the built site and deploys it. GitHub Pages has to use the GitHub Actions source for that deploy job. Product publication is a different workflow and stays manual.

The header wordmarks are `src/assets/conduit-wordmark-light.svg` and `src/assets/conduit-wordmark-dark.svg`. They use the console's three sapphire chevrons. `public/favicon.svg` is the console squircle mark.
