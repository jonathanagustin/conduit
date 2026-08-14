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

Credentials stay in the environment or the approved secret provider. It is
acceptable to load a repository `.env` file by reference when the user has
authorized publication, but never print, copy, commit, or include its values in
logs. A successful build or upload does not authorize deployment to a tenant.

Rollback means selecting a previously verified immutable artifact through the
owning deployment repository and approval path. Do not overwrite a published
tag to simulate rollback.
