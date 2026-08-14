# Metrics ledger

`metrics-ledger.jsonl` is an append-only event log used by the vendored Zenith
tools. Every nonblank line is one JSON object with `id`, `date`, `commit`,
`category`, `metric`, `value`, and `tier`.

Read a row in the context of its recorded commit. Older rows were retained
because editing or deleting committed measurements destroys the audit trail;
they do not describe the current tree unless a current measurement confirms
them.

Append measurements with the vendored recorder:

```bash
node tools/zenith-protocol/scripts/record-metric.mjs --help
```

Corrections append a new row with `supersedes`. Never revise or remove an
existing data line. Validate the ledger and the other repository records with:

```bash
node tools/zenith-protocol/scripts/conform.mjs
```
