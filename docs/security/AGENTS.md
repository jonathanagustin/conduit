# Security documentation rules

Use primary standards and upstream project documentation. State what the
repository integrates separately from what a deployment has configured or
proven.

Conduit owns authorization decisions, bounded provider-neutral evidence, and
verification of immutable artifact coordinates. Deployment repositories own
identity issuers, trust roots, credentials, secret-provider configuration,
registry access, scanners, and cluster operators.

Never commit credentials, private keys, raw scanner responses, tenant payloads,
vulnerability databases, or copied upstream manuals. Pin third-party tools and
record checksums or immutable source coordinates. Experimental or optional
features must not be described as stable defaults.

Preserve `PALANTIR-SECURITY-BULLETIN-REVIEW.md` as historical evidence. It is
not a current security policy or a substitute for a fresh advisory check.
