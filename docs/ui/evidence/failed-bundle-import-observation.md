# Failed Bundle import observation

This note records only the visible content of the public Apollo documentation
image `failed-bundle-import.png`, reviewed on 2026-08-07. The pinned inventory
row is `120-f9912a818379.png` in `../upstream-screen-evidence.tsv`, with SHA-256
`714da39649263a424bea493046e2529dd9256c8d150c20959e2a7e459ad6d3ef`.

The completed failure view shows eight ordered tasks with individual statuses:

1. Catalog delete operation import
2. Container image import
3. Module import
4. Product binary import
5. Product release import
6. Product release metadata import
7. Sls config import
8. Vulnerability suppression import

The sixth task is failed and the seventh and eighth are shown as succeeded. The
overall operation is shown as failed at 100 percent progress. This establishes
the visible task names, order, per-task status, and the fact that this captured
Apollo operation recorded later successful tasks after one failed task.

The image does not establish a private wire format, persistence model, retry
rule, transaction boundary, or requirement that Conduit continue after a
failure. Conduit aborts its atomic catalog application and uses `SKIPPED` for
later work that did not run; that is a Conduit-native divergence.
