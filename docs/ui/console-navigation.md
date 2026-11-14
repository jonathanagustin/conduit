# Operator console navigation

The workbench groups the live screens into six areas. An area opens on its overview. Tabs reveal the rest. The line above the tabs is the breadcrumb.

| Area | Hash | Tabs |
| --- | --- | --- |
| Packaging | `#area-packaging` | Timeline, Bundle, Proposals |
| Releases | `#area-releases` | Environments, Activity, Promotion, Recall |
| Change requests | `#area-changes` | Requests, Approvals |
| Intelligence | `#area-intelligence` | Logic, Evals, Courses, Automations |
| Fleet | `#area-fleet` | Command Center, Spokes, Drift |
| Audit | `#area-audit` | Hash chain |

Older hashes still open the same tab. `#s08-staged-run` is Packaging / Timeline. `#pt01-proposal-trace` is Packaging / Proposals. `#rm08-bundle-preview` is Packaging / Bundle. `#rm09-recalls` is Releases / Recall. `#s16-approvals-board` is Change requests / Approvals. `#au01-audit` is Audit / Hash chain. `#s13-fleet-overview` is Fleet / Command Center.

`scripts/demo-seed.sh` prints these hashes when it finishes.
