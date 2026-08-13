# GitHub workflows

- `main` is the only long-lived branch. Pull requests are squash-merged from
  short-lived branches after the manually dispatched Trunk gate passes.
- Keep Actions manual-only unless the user explicitly changes the cost policy.
  A required status check may require a manual dispatch; it does not justify a
  `push` or `pull_request` trigger.
- Apollo-facing pull requests must complete the tenant-fidelity record. Unknown
  behavior stays unknown rather than becoming a plausible implementation.
- Pin third-party Actions to full commit SHAs with version comments.
- Keep permissions least-privilege and publish immutable image descriptors even
  when an optional hosting feature such as GitHub attestations is unavailable.
- Pull requests build without publishing. Default-branch/tag runs may push only
  to the repository's platform packages.
- A release manifest must be assembled from registry-reported digests; never
  infer a digest from a tag.
- Keep CI, Spoke E2E, and OCI release workflows independently diagnosable.
