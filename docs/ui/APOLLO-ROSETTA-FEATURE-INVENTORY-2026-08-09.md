# Apollo Rosetta feature inventory

Status: exhaustive over the authenticated, digest-pinned Rosetta APOLLO corpus
available on 2026-08-09.

The source corpus contains 169 release-note records and 11 media items across
four Rosetta application identities. The 11 image bytes are durable in the
private OCI artifact pinned by
`mcp/foundry/evidence/rosetta-media/OCI-DIGESTS.json`; they hydrate to `/tmp` and
are exposed through an ignored symlink. The hash-keyed facts-only inventory is
`evidence/APOLLO-ROSETTA-FEATURE-INVENTORY.json`. It covers 11/11 catalog rows;
`scripts/verify-apollo-rosetta-inventory.mjs` fails on omission, duplication,
hash drift, or an empty feature/UI-pattern set.

The registry's non-prose metadata enumerates the four identities: Autopilot
(120 notes, 7 media), `apollo-cli` (37, 0), Aries App (10, 0), and Apollo
Deployment State (2, 4). It observes 125 versions and classifies the 169 notes
as 128 enhancements, 36 features, and 5 fixes. Those counts establish corpus
coverage only; release-note prose is deliberately not copied into this
repository.

## Feature families

| Family | Directly observed features |
| --- | --- |
| Environment administration | Kubernetes Namespace lifecycle, artifact/network/instance-group/notification settings navigation, Entity installation form, connection/editability state |
| Modules | Environment Module index, install/update choice, release channel and variables actions, Module contents/installations/settings navigation |
| Promotion | Ordered multi-stage Module pipeline, passed/evaluating states, revision/configuration/edit actions, per-channel ramp windows, gradual randomized rollout explanation |
| Governed access | JIT policy selection, YAML rule preview, fixed duration, request creation, attributable Change Request evidence and activity |
| Operations | Browser terminal, explicit identity/groups/context, You→Hub→Spoke latency, Kubernetes Pod output |
| Index composition | Search, filter/reload, dense tables, progress plus exact counts, relative age, status text/icons/tags, grouped row/resource actions |

## Platform disposition

- **Adopt/retain:** resource headers and tabs; secondary settings navigation;
  narrow summary projections feeding dense indexes; progress with exact counts;
  semantic promotion graphs; explicit evaluated/evaluating states; governed
  request evidence; and disabled/destructive actions that remain legible.
- **Already represented:** Environment namespaces (H26), Modules and promotion
  semantics (H6/H21/H35/H36), JIT boundary (H30), desired/reported state, and
  outbound Hub→Spoke operation evidence.
- **Improve:** expose pipeline revision history and configuration provenance in
  the console; make ramp windows visible alongside their realized rollout;
  preserve exact request expiry and activity; and paginate Module/release/event
  indexes before claiming scale.
- **Boundary:** the screenshot proves a terminal surface exists, not that an
  embedded shell is safe for this repository. H30 continues to require an
  external JIT access broker. The YAML shown in one policy is an example, not a
  universal permission model. No screenshot establishes a private route, DOM,
  transport, solver, or current tenant behavior.
