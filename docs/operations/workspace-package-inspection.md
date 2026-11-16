# Workspace packaging inspection failures

The reusable workspace packager may use an image builder and inspector. When an
inspector is configured, its failure is a required validation failure, not
permission to reuse the input/proposer digest. An empty inspection reference or
missing observed digest also fails that attempt. Package creation does not run
for an attempt whose inspection fails.

The existing repair limit still applies. Each attempt discards the previous
attempt's inspection result before building, so a later failed build cannot
retain an earlier successful inspection. A successful retry records its own
built reference and observed digest. Explicitly optional inspection remains
optional when neither the inspector nor the checks adapter supplies that service.
A declared input digest in that path is not described as an observed inspection.

Workspace audit metadata carries the packaging `outcome`. The CLI audit adapter
allows only `PASS`; failed or missing outcomes record a denial rather than the
former unconditional packaging-success event. Proposal commits remain available
for review when validation fails; they are not successful package archives.

Tests use controlled builder, inspector and package-check adapters to verify
failed inspection, missing digest, successful retry, stale-result removal and
the no-inspector contract. Those tests establish failure handling, not a live
image build or registry inspection. Native Hub workspace compute remains
unconfigured as described in [native workspace compute](native-workspace-compute.md).
The real Excalidraw build and packaging demonstration remains tracked by #615.
