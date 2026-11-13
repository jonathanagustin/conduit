# Release preparation

Release Please prepares reviewed version/changelog pull requests. It does not
release Conduit. [ADR 0010](../decisions/0010-release-preparation-with-manual-publication.md)
records the UDS sibling sources, Conduit adaptations and token permissions.

The initial manifest is `0.17.5`, matching the published `v0.17.5` tag at
`44e1e8845fe01c2a461061f6557a128ebf716af6`. Do not reset that manifest or
bootstrap from a newer arbitrary main commit. Future release PRs advance the
manifest together with VERSION, CHANGELOG.md and both Helm chart versions.

## Review and manual handoff

1. Main pushes run Prepare release. A new release-relevant commit opens or
   updates the single `release-please--branches--main` PR. Maintenance-only
   history produces no new release. Fixes/performance bump patch; features and
   breaking changes bump minor before 1.0.
2. Check the exact proposed VERSION, changelog and both charts. The preparation
   workflow explicitly dispatches Trunk because bot-token PR writes do not
   trigger normal PR CI. Wait for the required `Trunk gate` on that exact head.
3. Merge only with authorization, using the Conventional Commit release title
   and an explicit clean squash message. The merge publishes nothing.
4. To publish, deliberately dispatch the existing workflow against the reviewed
   main commit containing that VERSION:

   ```bash
   gh workflow run platform-release.yml \
     --repo jonathanagustin/conduit --ref main --field version=0.18.0
   ```

   Substitute the exact VERSION being released. Its preconditions still reject
   version disagreement, an existing tag, a non-main commit or missing/failed
   Trunk evidence. Follow `release-and-publication.md` for artifact verification.
5. After publication succeeds, the next Prepare release run verifies the real
   GitHub Release/tag and retires the merged preparation's pending label. Until
   then Release Please deliberately declines another release PR.

## Recovery

- Failed preparation or CI dispatch: repair the cause, then run
  `gh workflow run release-please.yml --repo jonathanagustin/conduit --ref main`.
  This updates the existing PR and schedules a missing/failed Trunk run even
  when Release Please reports no content change. Successful or active runs on
  the current head are left alone.
- A failed gate stays failed. Diagnose it before repeating; do not bypass the
  head authenticator or merge with missing checks.
- A PR creation permission error requires the repository's **Settings →
  Actions → General → Workflow permissions → Allow GitHub Actions to create
  and approve pull requests** setting. Enable only after the owner approves
  this integration. No App/PAT secret is needed; do not add one to fix a 403.
- A merged release PR waiting on publication is expected. Complete or repair
  the manual publication; do not clear `autorelease: pending` to force the next
  release. A tag without a published GitHub Release is not completion.
- If publication already succeeded but reconciliation failed, rerun preparation
  on main. Label updates are idempotent, and the tag/version/ancestry checks
  repeat before pending state is cleared.
- Never move an existing release tag or reset VERSION to repair a partial
  release. Use the existing release recovery procedure.

Run `./scripts/test-release-preparation.sh` for isolated real-engine and helper
fixtures. It performs no live GitHub writes and creates no real release.
