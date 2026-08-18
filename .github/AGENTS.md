# GitHub workflows

- `main` is the only long-lived branch. Pull requests are squash-merged from
  short-lived branches after the Trunk gate passes; opening or pushing the pull
  request starts that gate on its own (#195).
- Verification and maintenance workflows may run on their own under ADR 0006's
  guardrails: cancel superseded runs, cap runtime, least privilege, never
  publish. Publication, release, signing, and deployment stay
  `workflow_dispatch` only, and a green run authorizes none of them (A5).
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
