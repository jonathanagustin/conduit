# Native console journey verification

The `Native console journeys` workflow runs the existing browser harness against
a newly built loopback Hub and its embedded console. It checks out the actual
proposed head, records its commit and tree, and retains native process logs and
Playwright failure evidence. It is separate from `scripts/test-console.sh`, whose
browser suite intercepts Hub responses.

Relevant pull requests run verification automatically; manual dispatch can
repeat an unchanged branch. Superseded runs are cancelled, the job has a
45-minute limit, repository permissions are read-only, and third-party actions
and tool versions are pinned. No repository secret or external tenant is used.

The commands are also available locally:

```sh
./scripts/fetch-temporal-cli.sh
./scripts/simulate-console.sh --with-packaging
./scripts/simulate-console.sh --with-workspace-git --grep 'Git workspace history and confirmed fork'
```

Install the pinned Go/Node/pnpm toolchain, frozen console dependencies and
Playwright Chromium first. The packaging scenario uses the verified pinned
Temporal CLI and a recorded-fixture workflow. The second command covers the
existing Git-backed workspace history/fork journey; the healthy scenario uses
the separate metadata adapter. A missing fixture, refused response or failed
assertion fails verification rather than counting an empty catalog as success.

Set `CONDUIT_SIMULATION_EVIDENCE_DIR` to retain Hub, simulated Agent, model and
packaging/Temporal logs when those processes run. Hosted artifacts retain these
logs, exact source identities and browser traces/screenshots for fourteen days.
Read the workflow conclusion and actual test output together; an artifact's
existence alone does not mean the journeys passed.

These are Conduit-native fixture qualifications: seeded data, a stand-in model
and simulated Agent, with actual Hub authorization, persistence and HTTP
responses. They do not establish real-model quality, continuous soak, deployed
workloads, container isolation or full coverage of every shipped write. The
remaining write inventory and missing journeys stay tracked in #827, and
all-view Space scoping remains #823. Verification grants no publication or
deployment authority.
