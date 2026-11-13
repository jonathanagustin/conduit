# Prepare release pull requests while keeping publication manual

Status: Accepted (owner approved the scoped workflow token permissions, 2026-11-13)

## Context and sibling patterns

Issue #339 asks for Release Please based on inspected UDS sibling workflows:

- UDS CLI at `39e635a6a16234b69a8b1819ba8d42879a1a5799` uses a pinned
  `googleapis/release-please-action` with configuration/manifest files, then
  separately hands an explicit release tag to artifact production.
- UDS Core at `9c808fd0af6bd0dca138da05f4a0d4785f109c32` demonstrates a
  `skip-github-release: true` PR-only phase using `GITHUB_TOKEN`. Its other
  phase uses an App token to create releases/tags. Its configuration enumerates
  versioned files instead of replacing every version string in the repository.

Conduit's manual release workflow already owns tags and dispatches publication
only after preconditions pass. Copying the siblings' complete automation would
create a second tag owner and overturn ADR 0001/A5. Their release branches and
publication triggers are therefore not adopted.

## Decision

Prepare one Conventional Commit release PR on main with the pinned v5 action,
PR-only mode, a single manifest package and the simple strategy. `VERSION`
remains authoritative; JSONPath updates explicitly synchronize `version` and
`appVersion` in both Hub and Spoke Helm charts. Zarf's version is already a
publication template populated from VERSION. Web package versions, examples,
immutable evidence and dependency versions are not product release versions.

Bootstrap from the actual latest published release, `v0.17.5`, at
`44e1e8845fe01c2a461061f6557a128ebf716af6`. The initial manifest remains
`0.17.5`. The bootstrap SHA is a history floor, not a version reset or a
permanent last-release override. There is no retroactive changelog replay.

Pre-1.0 semantics are explicit: fix/performance commits bump patch; feature and
breaking commits bump minor. A breaking change does not silently promote the
product to 1.0. Hidden maintenance/documentation/test commits alone do not
prepare a release. Changelogs include features, fixes, performance and reverts.

## Token and CI boundary

Use only the built-in `GITHUB_TOKEN`; no PAT or App credential is introduced.
The preparation job requires `contents: write` for its branch,
`pull-requests: write` for PRs and their labels, and `actions: write` solely to
dispatch the existing `platform-ci.yml`. Default workflow permissions remain
read-only; there is no packages, signing, deployment, or environment authority.
Issue #339 explicitly requires owner approval before introducing these workflow
permissions; the owner approved this scope before merge.

Discovery uses the pull-request listing endpoint and filters pending labels
locally. The first live run (43372770820) showed that the repository-issues
listing endpoint requires separate issue-read authority; broadening permissions
to fix that 403 would violate the approved scope. A CLI fixture also covers PR
metadata larger than Node's default process-output buffer.

PRs created with `GITHUB_TOKEN` cannot be relied upon to start runnable
`pull_request` checks.
The handoff queries the managed PR's exact head, skips successful or active
Trunk runs for that head, and explicitly dispatches the existing gate when it
is missing or failed. The existing head authenticator proves the dispatched
branch and PR still name the same commit before any verification lane starts.
`workflow_dispatch` is exempt from the token's event-suppression rule.

A release-PR merge creates no tag, GitHub Release or artifact publication. Its
pending label intentionally prevents another release preparation until the
manual release is published. Before the next preparation, the helper verifies
an actual published GitHub Release, its lightweight tag, ancestry from the
merged PR, and matching VERSION at the tag. Only then does it mark that PR
`autorelease: tagged`, allowing the next release PR. A merge or tag alone is
insufficient. The helper's write allowlist admits PR labels and Trunk dispatch
only; tag, Release and publication endpoints are refused.

## Verification and limits

The isolated tests use Release Please 17.6.0, the engine locked by the pinned
v5.0.0 action. They execute the real manifest strategy against a fake GitHub
persistence boundary and cover version semantics, explicit files, metadata,
no-op behavior and repeated updates. Helper tests cover exact-head CI handoff,
failed-run recovery, publication reconciliation and forbidden writes. Parsed
workflow mutations prove that enabling release creation, adding publication,
broadening permissions, removing the ref/runtime guard or unpinning the action
is refused. Existing manual release/precondition guards remain in CI.

The owner approved the token scope and enabled GitHub's setting allowing Actions
to create pull requests. Live creation, unchanged-run idempotency and the explicit
exact-head Trunk handoff are recorded in `docs/operations/release-preparation.md`.
A permission error remains a setup failure, not a reason to introduce a credential
or skip the required gate.

## Owner-authorized history correction, 2026-11-13

The owner explicitly authorized rewriting historical metadata, including
published release tags. The `v0.17.5` commit is now `92e0c6f6258b68f7bebf5da04978ac54ec337844`
(previously `44e1e8845fe01c2a461061f6557a128ebf716af6`). Its file tree is identical.
Release Please's bootstrap and pinned verifier follow the corrected commit;
release versions, manifest values, publication authority, and artifacts are
unchanged. The [history correction record](../operations/history-correction-2026-11-13.md)
records the scope and source-SHA translation. This is a one-time metadata
correction, not permission to move tags during ordinary release recovery.
