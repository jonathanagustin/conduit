# Product and Release retirement

Source reviewed:

- [Delete a Product or Product Release](https://www.palantir.com/docs/apollo/managing-products/delete-product-and-product-release/)

## Public semantics adapted

- Product creators can request Product deletion and Release creators can
  request Product Release deletion.
- The operation is permanent. A Product cannot be deleted while one of its
  Releases is installed, and a Product Release cannot be deleted while an
  Entity runs it.
- Product deletion is reviewed through a Change Request that removes Product
  settings.

## Platform contract and divergence

Platform adapts permanent deletion as an immutable governed tombstone. It does
not erase Product/Release coordinates, prior desired states, Plans, reports, or
Change Request execution evidence. That deliberate divergence preserves the
repository's audit and rollback invariants and prevents a retired coordinate
from later acquiring different meaning.

`internal/controlplane/catalog_retirement.go` requires an attributable
rationale and Product `change:propose` authority, matching the public Product
creator and Release creator role capability. `RETIRE_PRODUCT` and
`RETIRE_PRODUCT_RELEASE` are explicitly enumerated Platform-native Change
Types. Immediate policy approval or a later eligible reviewer applies the exact
proposal and records terminal execution evidence.

Retirement fails while the latest desired Plan or reported state has a live
matching Entity, while a latest desired Release depends on the coordinate, or,
for Product retirement, while a Module installation or latest Module Release
still references the Product. These preconditions are rechecked atomically
when the tombstone is persisted. Plan creation and Module publication or update
recheck tombstones under the same repository lock; the public v0 publication
adapter also rejects sequential republishing. Dependency retirement is
deliberately conservative: any live range that contains the exact Release must
be changed before retirement.

Tests in `internal/controlplane/catalog_retirement_test.go` cover both scopes,
live Entity/report/dependency rejection, creator authorization, automatic and
reviewed Change Requests, restart persistence, coordinate non-reuse, Module
gates, and preservation of Plan/report history.

This is Apollo-aligned lifecycle behavior over Platform's native repository and
governance APIs. It is not compatibility with Apollo's private deletion or
catalog protocols, and Platform never claims physical erasure.
