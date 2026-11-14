# Reconstructed Apollo CLI 0.814.0 fixture

This fixture is reconstructed from Palantir's public Apollo documentation. It
has **not** been emitted by the tenant-distributed binary and is therefore a
semantic compatibility oracle, not a byte-for-byte golden file.

The reconstruction uses the documented output of
`apollo-cli product-release helm-chart init --as-tgz` and the 2026-07-22 CLI
changelog:

- Product archives contain `*/deployment/manifest.yml`;
- CLI 0.813.0 added `deployment/values.yaml` and
  `deployment/values.schema.json` to those archives;
- CLI 0.814.0 reads those configuration files back from the archive during
  Product Release creation;
- the current Helm Product type is `helm-chart.v1`.

`archive-files.txt` is the reconstructed normalized tar inventory. The
configuration files are copied from the deterministic input chart, and
`deployment/manifest.yml` is the expected semantic output for that chart.

When a real CLI is available, run `../../capture.sh`. CI compares captured
manifests and archive structure with this fixture while deliberately ignoring
YAML formatting, map order, tar timestamps, owners, and compression bytes.
