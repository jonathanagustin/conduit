# Apollo CLI golden fixtures

This directory is the native compatibility boundary for Apollo CLI-generated
artifacts. The current public changelog identifies CLI `0.814.0` (2026-07-22).
The binary is tenant-distributed from Apollo **Settings → Download** and is not
published in the public `palantir` GitHub organization.

Run the capture script with a tenant-downloaded binary:

```bash
APOLLO_CLI=/absolute/path/to/apollo-cli \
  docs/apollo/cli-fixtures/capture.sh
```

The script deliberately uses an empty temporary `HOME`, invokes only local
help/init/verify operations, and never reads or exports an Apollo profile. It
captures:

- the CLI version and command/flag surface;
- a Helm Product manifest generated from the deterministic fixture chart;
- the `--as-tgz` archive inventory and checksums;
- positive verification output for the generated manifest;
- negative verification outputs for intentionally invalid Product versions.

Generated results belong under `captured/<cli-version>/`. They may be committed
only after review confirms that they contain no credentials, tenant URLs, or
other tenant-specific metadata. CI will then round-trip our adapter against
those exact fixtures.

The capture retains the exact packaged input chart and native `.config.tgz`, as
well as SHA-256 hashes for the native executable and every captured output. It
does not retain the executable. To test a candidate implementation without
normalizing YAML, ordering, gzip headers, tar metadata, or terminal output:

```bash
docs/apollo/cli-fixtures/verify-byte-equivalence.sh \
  /absolute/path/to/captured/<cli-version> \
  /absolute/path/to/candidate-cli
```

Both `BYTE_EQUAL` checks must pass. No native capture is currently committed,
so byte equivalence is not yet established; the reconstructed fixture supports
semantic conformance only.

The known Foundry enrollment is not necessarily the Apollo Hub. Unauthenticated
probes to its Eddie repository correctly returned `401`; a bearer token or UI
download session is required before using the Artifacts route. Never commit or
paste that token into this repository.

## Reconstructed contract

`reconstructed/0.814.0/` is a provisional semantic fixture derived from the
official CLI reference, Product publishing guide, and 2026-07-22 changelog. It
allows conformance work to continue without pretending that reconstructed tar
bytes came from Palantir. Run:

```bash
ruby scripts/apollo-docs/validate-cli-fixtures.rb
```

The validator checks the documented manifest vocabulary, Apollo orderable
versions, OCI artifact declaration, Helm locator, 0.813+ archive configuration
files, and any real captures later placed under `captured/`.
