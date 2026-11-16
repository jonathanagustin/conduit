# Registry image inspection

The packaging registry adapter calculates SHA-256 over the exact manifest
response bytes. A digest-pinned request must match those bytes. A nonempty
`Docker-Content-Digest` header must also match; an absent header does not turn a
config digest into an image digest.

For an index, the returned image coordinate remains the verified index digest.
The selected child manifest is verified against its index descriptor before its
config is read. Config response bytes must match the manifest's SHA-256 config
descriptor, and a supplied config digest header must agree too. Any mismatch
returns an error without image inspection facts.

This verifies content identity, not publisher identity, provenance, signatures,
image-layer bytes or runtime behavior. Existing first-child index selection is
unchanged: the adapter does not qualify a platform selection policy. A successful
inspection alone does not prove that a workspace built the image, that package
references match it, or that a build was isolated. Those remain separate gates.

Regression coverage uses an HTTP registry with real content-derived digests:
matching and omitted headers, tag and pinned requests, index/child identity,
altered manifest/config bytes and false digest headers.
