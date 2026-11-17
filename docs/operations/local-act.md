# Local CI with act

The complete local verifier runs the CI gate set and reconciles it with
`platform-ci.yml`. `nektos/act` supplies an Actions-shaped entry point without
using GitHub-hosted runner minutes:

```bash
ACT_BIN=/path/to/act ./scripts/run-local-ci.sh "$(git rev-parse HEAD)"
```

Use act **v0.2.97**. The official Linux x86_64 release archive has SHA-256
`67b4976a56f33ea7870b798912e738ec44eef38e7578ae7b532f2def1c73a5ad`.
Install from `nektos/act`'s versioned release and verify its checksum; do not
commit the binary. Other architectures have their own official release checksums.
The runner uses act's host executor, so install the workflow's pinned Go, Node,
pnpm, Helm and required Ruby tools as described in `local-verification.md`.
Docker must be available for PostgreSQL and isolated fixture checks. This is a
local execution environment, not an emulation of GitHub's runner image.

The wrapper requires committed, clean candidate and tooling checkouts and an
exact expected commit. It runs the full verifier without a gate filter. The
workflow uses the custom `conduit-local` runner label and requires `ACT=true`;
it does not select a paid GitHub-hosted runner. No checkout action, implicit
`.env`/`.secrets` file or GitHub write token is passed to act. Repository tests
execute with the local operator's permissions; run only trusted candidate code.

Evidence is written outside the checkout to a unique temporary directory:
`act.log` plus a success-only `receipt.json` identifying candidate commit/tree,
tooling commit, workflow/verifier/runner/log hashes, tool versions and completion
time. The receipt also records the named Chromium profile, Node architecture,
Go compiler-host and target settings, QEMU guest-base setting, reported `uname`
values and Docker daemon architecture when available. Under emulation, `uname`
may report the guest architecture; the daemon value describes the container
server and may be remote. Failure or a changed checkout produces no success
receipt. The receipt is
operator-produced evidence, not an independent attestation or a hosted Actions
run. Retain the log with the receipt; a hash alone cannot explain a failure.

Committed tooling can also verify another clean worktree. From that worktree,
invoke the absolute path to `scripts/run-local-ci.sh` in the tooling checkout,
passing the candidate's exact SHA. The receipt distinguishes the two commits.
This lets unchanged proposed revisions be checked without adding tooling files
to their tree.

When the maintainer explicitly chooses local CI for a change, the complete
exact-head result can be used for merge review. Compare the current GitHub PR
head with the receipt before merging, and retain/link the result on that PR.
GitHub-required checks remain enforced; this command does not alter branch
protection, write a fabricated hosted status or override a server refusal.
Hosted run reporting depends on GitHub run IDs and remains separate. Release,
signing, publication and deployment retain their existing manual authorization
and release-path checks; a local PASS grants none of them.

Console verification gives Vite a cache under its own temporary run directory
through `PLATFORM_CONSOLE_CACHE_DIR`. The cache is a sibling of the build output,
so rebuilding the output cannot erase a live cache. The script removes its owned
run directory on exit, including failures. Ordinary developer runs retain Vite's
default cache. This cleanup does not remove installed dependencies, browser
binaries, qualified inputs, source, receipts or another run's cache.

For the Linux x86_64 act runner emulated on an ARM64 Podman VM, Chromium 1254's
zygote can crash despite installed browser OS dependencies. Select the bounded
local profile explicitly:

```bash
CONDUIT_LOCAL_CI_BROWSER_PROFILE=qemu-x86_64-no-zygote \
  ACT_BIN=/path/to/act ./scripts/run-local-ci.sh "$(git rev-parse HEAD)"
```

The profile requires act and a Linux x86_64 Node runtime. It adds exactly
`--no-zygote` to Chromium in the stubbed-console and live-Hub configurations:
Chromium starts its child processes directly. Browser contexts, renderer
processes, all assertions and zero retries remain in use. Other browser projects
and ordinary hosted/developer launches retain their defaults. Unknown profiles
and unsupported hosts fail rather than accepting arbitrary browser flags.
This profile does not fix an unusable Go race runtime; its fail-closed preflight
must also pass. A browser smoke probe alone is insufficient: retain the complete
console and real-Hub results in the full exact-head act log before using a PASS
receipt for review.
