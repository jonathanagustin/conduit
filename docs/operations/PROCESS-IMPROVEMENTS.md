# Process improvements

Method changes move through `PROPOSED`, `TRIAL`, and `ADOPTED`, or close as
`REJECTED` or `SUPERSEDED`. Adoption requires a recorded measure and a whole-
system cost check. Votes are observations, not adoption evidence.

| PDR | Change | Status | Exit criterion | Measure | Holistic check | Votes |
| --- | --- | --- | --- | --- | --- | --- |
| P1 | Keep living documentation separate from immutable evidence and machine ledgers. | TRIAL | Three substantial changes finish with zero references to retired living documents and no unexplained evidence mutations. | Baseline: this curation reduced `docs/` from 217 to 132 files, replaced 20 architecture narratives with 9 current documents, removed 66 evaluator-run files, and left zero references to deleted paths. The independent review found one 697-line plan mislabeled as evidence; the follow-up removed it and retained one bounded observation. | Review lookup cost, repository size, provenance, and the risk of deleting unique evidence. | good×0; bad×1 |
| P2 | Accept generated console assets only after the Linux production-build comparison passes. | TRIAL | Three console-affecting changes finish with `scripts/test-console.sh` reporting a byte-identical embedded bundle. | The first cross-platform build produced 13 differing asset paths; regenerating under Linux reduced the mismatch to zero and the gate passed with 28 unit and 77 browser tests. | Linux verification adds runtime and a browser cache, but prevents platform-specific bytes from entering the embedded production bundle. | good×1; bad×0 |

For a new PDR, name the method change, trial length, objective exit measure, and
cost dimensions before applying it. Keep one aggregate `good×N` and `bad×N`
tally in the vote cell. Record the underlying observations in the change or an
append-only evidence artifact.
