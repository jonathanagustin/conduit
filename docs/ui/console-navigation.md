# Operator console navigation

Checked against `web/src/console/operator/engine.js` on 2026-11-17. The `AREAS`
table in that file is the authority; this page copies it and is wrong wherever
the two differ.

The workbench groups its screens into seven areas. The left rail's NAVIGATION
group lists them in this order. An area opens on its first tab. The hash names
the area only (`#area-releases`); the selected tab is not in the hash.

| Area | Hash | Tabs, in order |
| --- | --- | --- |
| Command Center | `#area-command` | Overview, Example fleet, Release Control Tower |
| Packaging | `#area-packaging` | Runs, Timeline, Pipeline, Workspace, Source, Preview, Lifecycle, Bundle, Proposals |
| Releases | `#area-releases` | Environments, Products, Dependencies, Plans, Channels, Waves, Windows, Planning, Ramp, Convergence, Activity, Promotion, Recall |
| Change requests | `#area-changes` | Requests, Approvals, Diff, Constraint gate |
| Intelligence | `#area-intelligence` | Logic, Evals, Courses, Automations |
| Fleet | `#area-fleet` | Environments, Drift, Spoke operations |
| Audit | `#area-audit` | Hash chain, Platform status |

## The rest of the left rail

Below NAVIGATION the rail lists screens that are not areas and have no tabs.

| Group | Entry | Hash |
| --- | --- | --- |
| BUILDER & STUDIO | Work Intake & Stories | `#s01-intake` |
| BUILDER & STUDIO | Workspaces | `#s02-workspaces` (opens Packaging / Workspace) |
| BUILDER & STUDIO | Active Work Runner | `#s06-active-work` |
| OPERATOR TOOLS | Proposal Trace | `#pt01-proposal-trace` (opens Packaging / Proposals) |
| OPERATOR TOOLS | Proposals & Evals | `#pt02-proposals-evals` (opens Packaging / Proposals) |
| OPERATOR TOOLS | Debug Terminal | `#rm11-debug-terminal` |
| GOVERNANCE | Ontology | `#s19-ontology` |
| GOVERNANCE | Component System | `#s18-components` |
| GOVERNANCE | Preferences | `#adm01-user-prefs` |
| GOVERNANCE | Organization | `#adm02-org-control-panel` |
| GOVERNANCE | Data sources | `#adm04-data-sources` |
| GOVERNANCE | Sign-in | `#adm03-auth-signin`, listed only when the engine's `authEnabled` state is set |
| GOVERNANCE | Notifications | `#ntf-inbox` |

## Capabilities

Navigation follows the session's capabilities
(`contracts/session-capabilities.md`, `session-capabilities.js`). An area or
tab that maps to an application the session does not hold is left out of the
rail, and opening its hash shows why it is unavailable instead of the screen.
An entry with no mapping is always shown. While the capability read is loading
or has failed, mapped entries are hidden.

## Screen hashes

A tab's own screen id opens that tab: `#s08-staged-run` is Packaging /
Timeline, `#rm08-bundle-preview` is Packaging / Bundle, `#rm09-recalls` is
Releases / Recall, `#s16-approvals-board` is Change requests / Approvals,
`#au01-audit` is Audit / Hash chain. The hash is then rewritten to the area's.

Two screen ids are the screen of two tabs. The later row of `AREAS` wins:

- `#s15-plan-detail` opens Releases / Planning, not Command Center / Release
  Control Tower.
- `#s14-env-detail` opens Fleet / Drift, not Releases / Environments.

Five ids are mapped explicitly after the table is read:
`#s13-fleet-overview` opens Command Center / Overview (the live overview, not
the Example fleet tab whose screen it is), `#pt02-proposals-evals` opens
Packaging / Proposals, `#s10-diff` opens Change requests / Diff,
`#rm01-env-page` opens Releases / Environments, and `#rm05-release-rollout`
opens Releases / Convergence.

One hash carries a parameter. `#s01-intake/<sessionId>` opens Work Intake on that planning session: the console reads `GET /v1/planning-sessions/{id}` and its `/events`, and says so when the Hub no longer has the session. The page writes this hash itself once a session exists, so a reload or a copied link returns to it.

`scripts/demo-seed.sh` prints the area hashes when it finishes.
