# Download-asset declarations

The Product assets context prepares immutable publisher-declared download
metadata. It is a Conduit extension, identified by
`conduit.delivery/download-assets/v1`, `CONDUIT_NATIVE` and
`PUBLISHER_DECLARED`. It is distinct from the per-Release Libraries inventory
and its unresolved verification vocabulary in issue #133.

A declaration names a Product and stable version, one to 64 download assets,
credential-free HTTPS coordinates and declared SHA-256 hashes. Optional fields
record OS/architecture targets, byte sizes, source commit and signature or
provenance bundle locations. The context does not fetch these coordinates or
verify their claims. Its canonical content digest binds the declaration, not
an independently observed archive. Sorting assets by name makes identity
independent of input ordering; duplicate names, including case variants, and
duplicate declared targets are rejected.

Preparation records the caller subject and server time after publication
authority succeeds. The immutable storage key is the Product/version
coordinate. An identical retry returns the original preparation provenance;
changed content conflicts. File storage uses private records and exclusive
installation, and PostgreSQL uses its own migration component and table. An
older delivery snapshot writer cannot erase these records.

Prepared intent is not a publication. The application requires a matching
catalogue receipt, including Product, version, kind, content digest and
publication time, before a read succeeds. A failed catalogue write can leave
prepared intent for a retry. A committed receipt without its matching metadata
is corruption, not an empty catalogue. Optional Space grouping reserves an
immutable anchor before catalogue commit and grants no publication permission.
Read authority is required independently; a write-only retry returns a minimal
acknowledgement without exposing the first publisher's identity.

This foundation has no production composition, HTTP route or CLI command yet.
It does not publish Conduit, download artifacts, authorize Agent execution,
verify signatures or create a deployment. Issue #193 remains open for those
separate catalogue and client integrations and the explicitly authorized
operational steps.
