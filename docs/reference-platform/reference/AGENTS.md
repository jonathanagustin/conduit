# Apollo reference notes

This subtree is the compact, agent-oriented index of facts derived from public
Apollo material. The parent evidence rules remain authoritative.

- Start with `README.md`, then read only the topic note needed for the task.
- For every reviewed upstream page, add or update `review-ledger.tsv` with its
  canonical URL, UTC review date, snapshot SHA-256 from the parent manifest,
  topic note, and disposition.
- Topic notes contain paraphrased contracts, uncertainties, and links to local
  implementation/tests. Do not mirror upstream page bodies or screenshots.
- When a contract changes code, update the topic note and
  `../PUBLIC-DOCUMENTATION-COVERAGE.md` in the same change. A claim becomes
  Implemented only after an automated test exists.
- Preserve the distinction between public API compatibility, semantic
  alignment, and private protocol boundaries.
