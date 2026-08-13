# Public Apollo contract validation

The public Apollo OpenAPI document is an upstream drift detector and vocabulary
source. An unmodified, licensed snapshot is pinned under
[`snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c`](snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c)
for repeatable validation. Capturing it does not make the Go delivery control
plane API-compatible with Apollo.

The exact automated claims and the native-interoperability boundary are listed
in [`CONFORMANCE.md`](CONFORMANCE.md).

The complete public-documentation review is recorded in
[`PUBLIC-DOCUMENTATION-COVERAGE.md`](PUBLIC-DOCUMENTATION-COVERAGE.md). Its
companion manifest inventories and hashes all 241 official pages captured on
2026-08-01 without vendoring their source bodies.

Compact source-derived facts are routed through
[`reference/`](reference/README.md). Authorized private-wire evidence has a
separate fail-closed intake under
[`private-interop/`](private-interop/README.md), and native CLI byte evidence is
kept under [`cli-fixtures/`](cli-fixtures/README.md).

Implemented reconstructed contracts are progressively disclosed in focused
notes: [`GOVERNANCE-CONTRACT.md`](GOVERNANCE-CONTRACT.md),
[`BUNDLE-CONTRACT.md`](BUNDLE-CONTRACT.md), and
[`FLEET-REPORTED-STATE.md`](FLEET-REPORTED-STATE.md).

Refresh the inventory into a temporary file before reviewing upstream drift:

```bash
node docs/apollo/snapshot-public-documentation.mjs \
  /tmp/apollo-public-documentation-manifest.tsv

diff -u docs/apollo/public-documentation-manifest.tsv \
  /tmp/apollo-public-documentation-manifest.tsv
```

Validate the pinned contract:

```bash
ruby docs/apollo/validate-public-api.rb \
  docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c/api-definition.yml
```

To review upstream drift, validate a fresh shallow checkout of `develop` and
compare it with the pinned snapshot:

```bash
git clone --depth=1 --branch develop \
  https://github.com/palantir/apollo-openapi.git /tmp/apollo-openapi

ruby docs/apollo/validate-public-api.rb \
  /tmp/apollo-openapi/openapi/api-definition.yml
```

The validator resolves every local `$ref` and asserts the Helm Product,
dependency locator, and authentication schema names used by the deployment
model. A failure means the upstream contract must be reviewed; it is not fixed
by loosening validation blindly.

The FedStart reference repository should likewise be inspected as a shallow,
external checkout:

```bash
git clone --depth=1 https://github.com/palantir/fedstart-helm-charts \
  /tmp/fedstart-helm-charts
```

FedStart informs release discipline: independently packaged charts, OCI
publication, specific image tags, and release-specific mirrored repository
overrides. Its charts are references, not vendored foundation dependencies.
