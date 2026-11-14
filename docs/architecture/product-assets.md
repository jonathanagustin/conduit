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

The optional Hub `ProductAssets` store mounts three native operations:

- `POST /v1/product-publications/download-assets` accepts one strict declaration.
- `GET /v1/products/{productID}/releases/{version}/download-assets` reads its receipt-bound metadata.
- `GET /v1/products/{productID}/download-releases` lists published metadata.

Publication requires artifact creation, Product creation when the Product is
new, and Release creation. Reads require the Product resource-read operation.
Both use the original credential carrier, including delegation restrictions.
An optional publication `spaceId` must identify a readable persisted Space and
reserves immutable Product grouping before catalogue commit. It grants no
publication permission. The native catalogue receipt retains ordinary stable
version channels and has no executable Release definition. Existing retirement
tombstones hide matching records and prevent republication.

Publication-only catalogue receipts establish Product and Release identity for
the existing governed retirement command, including download and Terraform
publications that have no executable Release. The preconditions still check
live Entities, Plans, dependencies and Module references. Retirement preserves
publication metadata and history; it does not grant permission to publish a
replacement at that coordinate. Retirement map keys use the validated Maven
coordinate grammar; the previous NUL separator could not be persisted in
PostgreSQL JSONB. Existing file record names remain unchanged.
Older PostgreSQL Hub binaries use the former map key and cannot enforce newly
persisted retirement entries. Upgrade every Hub writer sharing that database
before using this newly working retirement operation. Independent download
metadata survives legacy snapshot writes; that data-preservation guarantee does
not establish old-runtime retirement enforcement.

Production startup opens a separate `product-assets` component after the
packaging-worker early return, matching the configured file or PostgreSQL
repository mode. PostgreSQL errors are sanitized before startup diagnostics.
The restore drill includes both the declaration and its catalogue receipt and
checks the migration ledger before constructing the restored component.
The native CLI group is `product-release download-assets`, with `publish`,
`get` and `list`. Publish accepts strict JSON and supports an offline `--dry-run`
that never creates a client or contacts the Hub. Unsupported output formats
are rejected before publication. Space selection is forwarded only on publish.
The self-Product preparer emits a version-2 CLI declaration after checking all
six archive bytes and their source/target/version/builder metadata. Signature
and provenance bundle URLs are declared coordinates, not verification results.

The context does not publish Conduit automatically, download artifacts,
authorize Agent execution, verify signatures or create a deployment. Issue #193
remains open for the remaining integration and explicitly authorized
operational steps.
