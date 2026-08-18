# Release and publication

Release and publication require an explicit instruction separate from approval
to implement or merge code. The repository is proprietary. Publish only to the
approved private destination and preserve third-party license notices.

Before a release:

1. identify the exact commit and semantic tag;
2. run local verification and a deliberately dispatched release gate;
3. confirm the target registry, repository visibility, and retention policy;
4. publish immutable digests with the available SBOM, provenance, signature,
   and attestation artifacts;
5. verify the published objects by digest from a clean client.

## Cutting a release: one instruction

Land the `VERSION` bump the ordinary way — pull request, green Trunk gate,
squash merge — and then give one instruction:

```bash
gh workflow run platform-release.yml \
  --repo jonathanagustin/conduit \
  --ref main \
  --field version=<X.Y.Z>
```

That dispatch is the release decision, and it is the only one. The workflow
refuses before it creates anything unless all of the following hold for the
commit `main` points at:

- the version given equals `VERSION` at that commit, spelled as three numbers
  with no leading `v` (the Flux source policy trusts only
  `refs/tags/v[0-9]+.[0-9]+.[0-9]+`, so a prerelease tag would publish artifacts
  production then refuses to verify);
- `vX.Y.Z` does not already exist — a published tag is an identity, never moved;
- the commit is contained in `main`;
- the `Trunk gate` commit status is `success`. Pull requests are squash-merged,
  so the commit on `main` is an object no gate has seen; the verdict lives on
  the pull-request head the gate authenticated, and the check follows the merge
  back to it when exactly one merged pull request names this commit as its merge
  commit.

Only then does it create `vX.Y.Z`, read the tag back to confirm it names the
verified commit, and dispatch `platform-images.yml` against the tag. It does not
wait for publication: that run is tens of minutes and a second job watching it
bills a second runner for all of them. Watch the publication run itself, and
verify the published objects by digest as above.

Publication stays manual (ADR 0001; A5). One instruction, never zero: nothing
here is triggered by a push, a schedule, or a merge.
`scripts/verify-release-workflow.rb` holds both workflows to that shape and its
self-test exercises each refusal, because a release path can only be rehearsed
against mutated inputs — cutting a real release to test it is the thing being
tested.

Dispatching `platform-images.yml` by hand is no longer a supported release step
and now fails immediately from any ref that is not a `vX.Y.Z` tag agreeing with
`VERSION`. It used to succeed there: the image push was not tag-gated while the
runtime source, charts, air-gap packages, CLI and GitHub release were, so a
branch dispatch published `<component>-sha-<commit>` images, skipped everything
else, and reported success for a fraction of a release.

Credentials stay in the environment or the approved secret provider. It is
acceptable to load a repository `.env` file by reference when the user has
authorized publication, but never print, copy, commit, or include its values in
logs. A successful build or upload does not authorize deployment to a tenant.

Rollback means selecting a previously verified immutable artifact through the
owning deployment repository and approval path. Do not overwrite a published
tag to simulate rollback.
