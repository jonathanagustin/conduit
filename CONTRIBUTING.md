# Contributing to Conduit

Conduit uses a protected, trunk-based workflow. `main` is the only long-lived
branch. Work on a short-lived branch, open a pull request early, and merge a
small coherent change by squash after its evidence and verification are
reviewed. Release branches and tenant-specific code branches are not used.

## Tenant fidelity

Observable Apollo behavior is a compatibility target, not permission to fill
gaps by intuition. Every Apollo-facing change must classify each material claim
using the vocabulary in
[`docs/operations/TENANT-FIDELITY.md`](docs/operations/TENANT-FIDELITY.md):

- **Public contract** — supported by pinned first-party public documentation,
  OpenAPI, Conjure, CLI output, or source.
- **Tenant observation** — supported by an authorized, sanitized observation
  whose scope and capture time are recorded.
- **Conduit extension** — deliberately broader or different behavior owned by
  Conduit and never presented as Apollo behavior.
- **Unknown** — not established. Record it as a hypothesis or boundary; do not
  implement or document it as if it were known.

Transport interfaces are generated from the pinned OpenAPI or Conjure contract
when one exists. Handwritten domain models may sit behind the generated
boundary, but handwritten request/response types must not silently replace it.
Raw tenant URLs, identifiers, payloads, credentials, cookies, tokens, binaries,
or private source never enter Git.

## Change flow

1. Update local `main` and create a branch named `feat/<subject>`,
   `fix/<subject>`, `docs/<subject>`, or `chore/<subject>`.
2. State the observable outcome and evidence class in the pull request.
3. Add or update the smallest executable contract that would fail if the claim
   were invented or the behavior drifted.
4. Run the relevant focused checks locally while iterating.
5. Run `./scripts/verify-local.sh` before requesting merge.
6. Manually dispatch the **Trunk gate** workflow against the pull-request
   branch. GitHub Actions are intentionally not triggered by pushes or pull
   requests.
7. Squash-merge only after the required trunk gate passes and every explicit
   divergence is recorded. Delete the branch after merge.

Do not stack dependent pull requests unless each earlier slice can merge and
ship independently. Rebase a branch that has drifted from `main`; do not merge
`main` into it.

## Pull-request size and generated output

A pull request should represent one independently reversible decision. Generated
output and the source contract that produced it land in the same pull request.
Review the semantic contract diff, not only the generated file count. A
generator must be deterministic and its verification command must fail when the
committed output is stale.

## Releases

Releases are cut from an exact commit already on `main`. Publication workflows
remain separately and manually authorized; merging a pull request never
publishes an image, package, or deployment.
