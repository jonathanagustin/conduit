# Export Pipeline data-selection contracts

Source identity is recorded in `review-ledger.tsv`. The local Bundle envelope
is Platform-native and signed; no Apollo-private Bundle bytes are claimed.

## Derived contract

- Rules select Environments, latest Module definitions, Products, exact
  Product Releases, or Teams. Environment and Module selectors derive their
  Product set; Product and Module release selection requires explicit Release
  Channels. Exact Release and Team selectors do not accept a release strategy.
- `NONE` exports the selected parent resource without Product Releases.
  `ALL` exports the latest matching Release per Product/Channel in an
  initialization snapshot and all newly published matching Releases in later
  incrementals. `MINIMAL` selects the newest matching Release per
  Product/Channel and closes required dependency bounds with the smallest
  eligible set available to the catalog.
- Snapshot Pipelines always describe current selected state. Incremental
  Pipelines compare the full current selection with their prior lineage and
  emit additions, updates, and deletion tombstones. Team updates therefore
  appear in subsequent Bundles.
- Required/excluded labels are applied before dependency closure; a required
  dependency that fails the same label policy fails closed instead of entering
  a Bundle through the back door.

Executable evidence:

```text
go test ./internal/controlplane -run 'TestBundleInitializationSelectsLatestReleaseAndExportsTeams|TestBundleSnapshotIncrementalCheckpoint|TestBundleLabelRequirements'
```
