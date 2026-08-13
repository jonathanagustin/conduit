# OCI release contract

Every platform Git tag publishes two multi-architecture OCI images:

- `ghcr.io/jonathanagustin/platform-hub`
- `ghcr.io/jonathanagustin/platform-spoke-agent`

Release tags are convenience selectors. Deployments consume the immutable
`image@sha256:...` references recorded in the release asset
`platform-oci-manifest.json`. Its `platform.delivery/oci-release/v2` contract
binds both images to one platform version and Git commit plus the two
architecture-specific SBOM and vulnerability-report hashes.

Images are built for `linux/amd64` and `linux/arm64` from tag-and-digest pinned
base images. Their OCI labels and the Hub's unauthenticated `GET /version`
endpoint expose the version, commit, and commit-derived build timestamp. Build
provenance and a BuildKit SBOM are attached to each OCI manifest. The release
workflow additionally runs archive-hash-pinned Syft against both platform
manifests selected from the exact image index, publishes native and SPDX SBOMs
for `linux/amd64` and `linux/arm64`, scans each SBOM with archive-hash-pinned Grype,
and fails on High or Critical findings. The Grype JSON retains the scanner and
database descriptor; `platform-oci-manifest.json` binds SHA-256 hashes of the
SPDX and scan files to each image. OCI artifact referrers bind each pushed
digest to its BuildKit provenance and software bill of materials. The workflow
also applies Sigstore keyless signatures plus SPDX and Grype attestations to each
immutable image digest and to the reusable Flux runtime digest. The signing
certificate binds the signature to the repository's `images.yml` workflow and
GitHub Actions OIDC issuer. GitHub's repository-level attestation API is
unavailable for user-owned private repositories, so the workflow does not
depend on it. Standalone CLI archives instead carry portable keyless Sigstore
signature and SLSA v1 provenance bundles, and the workflow verifies each
archive-bound provenance bundle before publication.

The packages are private with the repository. A Spoke or downstream CI must
authenticate to `ghcr.io` before pulling. Kubernetes deployments should use an
`imagePullSecret`; local Docker/Helm workflows can use a narrowly scoped GitHub
token with package read access.

The release process is:

1. Update `VERSION` in the reviewed pull request.
2. Merge only after CI and pull-request image builds pass.
3. Create and push the annotated `platform/vMAJOR.MINOR.PATCH` tag on the merge
   commit. The namespace prevents collisions with other Palantir components.
4. Wait for the `OCI images` workflow to publish both architectures, keyless
   signatures, attestations, and `platform-oci-manifest.json`.
5. Verify each immutable reference before mirroring or deployment. For a tagged
   release, use the exact workflow identity and GitHub OIDC issuer:

   ```bash
   identity="https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/platform/v${version}"

   cosign verify \
     --certificate-identity "${identity}" \
     --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
     "${image_reference}"

   cosign verify-attestation \
     --type spdxjson \
     --certificate-identity "${identity}" \
     --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
     "${image_reference}"
   ```

   **Releases up to and including `v0.14.0` carry a different identity** and do
   not verify against the one above. They were built while this tree was its own
   repository, so their certificate says:

   ```
   https://github.com/jonathanagustin/conduit/.github/workflows/images.yml@refs/tags/v${version}
   ```

   Both are correct for what they signed; keyless identity records the workflow
   that ran, so moving the workflow necessarily changes it. There is nothing to
   re-sign -- an old artifact verified against the new identity is a real
   mismatch, not a stale document.

6. Update downstream source pins from that manifest, never by resolving a
   mutable tag at deployment time.

Private or disconnected mirrors may re-sign copied digests with an
organization-controlled KMS key. That signature is additional deployment
authority; it does not change the source digest or authorize Platform to own
the organization's key.

Renovate maintains the pinned Docker base digests and GitHub Action commits.
These dependency changes remain subject to the same tests and image build as
application code.
