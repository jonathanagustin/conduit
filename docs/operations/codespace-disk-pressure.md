# Codespace disk pressure

Conduit treats build caches as reconstructible and repository state as durable.
Only caches may be redirected to `/tmp`; source, Git refs, evidence, receipts,
configuration, and credentials remain under persistent storage.

## Restart invariant

A Codespace stop preserves absolute symlinks but clears `/tmp`. The first shell
after a stop must therefore recreate approved directory targets before Go,
Helm, Playwright, mise, or Cursor attempts to create a directory through a
dangling link.

`scripts/repair-volatile-cache-links.sh` repairs only existing absolute symlinks
whose targets are below the configured volatile root. It refuses an external
target, never creates or replaces a link, and never removes content.

`install.sh` installs an idempotent `.bashrc` block in Codespaces. The block
starts `scripts/codespace-self-heal.sh` in the background. A marker and lock
under `/tmp/conduit-self-heal` make it once-per-volatile-disk-lifetime while
allowing a new run after `/tmp` is cleared. A failed repair leaves no completion
marker, so a later shell retries.

## Verification

```bash
./scripts/test-repair-volatile-cache-links.sh
./scripts/test-codespace-self-heal.sh
```

The first test proves target validation, all six cache classes, and idempotence.
The second deletes both the volatile targets and lifetime marker, proves the
next invocation repairs them, and installs the shell block twice while
retaining exactly one copy.

This mechanism repairs cache directories only. Private OCI offload of durable
artifacts has a separate evidence and authorization boundary; a cache repair is
not proof that a release or receipt was published.
