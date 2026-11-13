# Public documentation inventory refresh

The 2026-11-13 network crawl completed over the published Apollo documentation
from the four repository-defined seeds. It found 284 pages: 22 added, none
removed, and 173 with changed source hashes compared with the prior inventory.
Canonical URLs and current SHA-256 hashes are in
[`../public-documentation-manifest.tsv`](../public-documentation-manifest.tsv).

This is an inventory verification, not a semantic review of 284 contracts.
The appended review-ledger entries explicitly carry that limitation. Existing
implementation claims and their earlier semantic reviews retain their original
scope; changed page content does not automatically authorize new behavior.
No upstream page bodies or tenant material are retained.

The crawler extracts Markdown from the site's published page data, follows the
Apollo path graph to closure, and refuses incomplete or contentless captures.
The offline freshness gate now measures this successful crawl rather than
changing its 45-day limit. Existing contract and citation gates remain required.

Validation: `node scripts/verify-documentation-freshness.mjs`,
`node scripts/verify-documentation-claims.mjs`, and the Trunk gate.
