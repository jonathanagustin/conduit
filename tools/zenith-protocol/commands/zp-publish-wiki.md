---
description: Publish canonical Zenith optimization articles to a GitHub Wiki with source-commit provenance.
---

Publish the reusable Zenith articles to the GitHub Wiki named by `$ARGUMENTS`, defaulting to
`drengskapur/agentdriver`.

1. Run `node ${CLAUDE_PLUGIN_ROOT}/scripts/publish-wiki.mjs --check`.
2. Confirm `GITHUB_TOKEN` is available without printing it. Set `ZENITH_WIKI_OWNER` and
   `ZENITH_WIKI_REPOSITORY` when the argument names another repository.
3. Run `node ${CLAUDE_PLUGIN_ROOT}/scripts/publish-wiki.mjs`.
4. If GitHub reports the separate wiki Git repository is uninitialized, stop and provide the exact
   one-time Home-page URL emitted by the script. Do not use an undocumented page endpoint.
5. Report the published source commit and article count. The repository sources remain authoritative;
   the Wiki is a human-readable projection.
