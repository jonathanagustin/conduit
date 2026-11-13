# Apollo release notes: the tenant's record versus the public one

Captured 2026-08-15 from the tenant's Rosetta service (`/documentation/api`),
the platform's own documentation service, which serves release notes to the
Foundry applications that render them.

## Why this exists

`core/whats-new` is the public Apollo changelog and was mined on the same day.
Rosetta carries the same kind of record for the same product, and the two are
not the same size:

| | Public `core/whats-new` | Tenant Rosetta APOLLO |
| --- | ---: | ---: |
| Dated entries / notes | 120 | **178** |
| Release versions named | 42 (`apollo-cli`) | **134** (all surfaces) |
| — of which CLI-shaped `vN.N.N` | 42 | **48** |
| Distinct dates | 120 | 116 |

**Corrected 2026-08-17.** The middle row used to read "Apollo CLI versions
named: 42 / 134", and the headline under it used to say *"93 CLI versions appear
in Rosetta and never on the public page."* That was a category error, and it
overstated the divergence by roughly a factor of two.

Recomputed from the committed registry: 134 is the count of **all** distinct
`releaseVersion` strings, and only **48** of them are bare `vN.N.N`. The other
86 are `surface@version` — `autopilot@`, `noc-inbox@`,
`apollo-build-factory-app@` and five more. The 93 is exactly `134 − 42 + 1`:
Rosetta's whole-platform version set differenced against the public page's
`apollo-cli` versions. Like for like, the CLI-version divergence is bounded
above by 48.

`scripts/verify-rosetta-release-notes.mjs` now prints the CLI-shaped count on
every run, so the two sets cannot be conflated again by someone reading only the
summary block.

Unchanged, and still believed: 58 Rosetta notes fall on dates the public page
has no entry for at all, and 46 public dates carry no Rosetta note — so neither
record is a superset. **Neither figure can be recomputed from this repository**,
because the public page is committed only as a manifest row (URL, word count,
one SHA-256). The instruction further down to recompute rather than quote from
memory therefore cannot be followed offline by anyone; it needs a live fetch.

The tenant record is still the larger by note count, and the shipping surfaces
below are content the public page does not carry at all. That was never in
doubt — only the version arithmetic was wrong.

The newest note on both sides is dated 2026-07-22, and the top of each record
agrees closely: the same CLI versions, the same subjects. They diverge going
backwards. The public page is an edited view, not a mirror, and a repository
that reads only the public page is reading the edit.

## What the divergence contains

The Rosetta-only notes are concentrated in two places the public page barely
covers.

**Application identities.** The public changelog speaks only of "Apollo" and
`apollo-cli`. Rosetta attributes notes to the shipping surfaces, which is how a
UI change is distinguished from a control-plane one. There are **eight**, not
the three this paragraph used to name — counted from the committed registry on
2026-08-17:

| Surface | Notes | Distinct versions |
| --- | ---: | ---: |
| `autopilot@` | 87 | 60 |
| bare `vN.N.N` (CLI-shaped) | 48 | 48 |
| `noc-inbox@` | 10 | 6 |
| `apollo-build-factory-app@` | 10 | 7 |
| `apollo-ui-docs@` | 7 | 4 |
| `mission-manager-toolbox-app@` | 6 | 4 |
| `aries-app@` | 3 | 2 |
| `hyperion-downdraft-form@` | 2 | 2 |
| `apollo-mesh-manager-app@` | 1 | 1 |
| no `releaseVersion` | 4 | — |

Two of these — `mission-manager-toolbox-app` and `aries-app` — appear nowhere
else in this repository: not in the RBAC capture, not in any prose, and this
repository cannot say what they are. `apollo-build-factory-app` and
`apollo-mesh-manager-app` are corroborated by role families in the tenant
authorization capture (`internal/apollo/roles.gen.go`) and by nothing else.
`noc-inbox` is a shipping notification inbox, a product the public Apollo
documentation corpus does not document at all — see
`docs/apollo/reference/notifications.md`, which records that no public page was
captured for it.

The `byApplication` summary field says `{core: 120}` while the file carries 178
rows; the remaining 58 have no `applicationId`. That field was bound to nothing
by the verifier until 2026-08-17 — it is the one summary value that names
surfaces, and a reader could have taken it for the scope of the capture. It is
now checked against the rows, and the unattributed count is disclosed.

**The federation contract.** A cluster of Rosetta-only notes from 2025-08 and
2025-09 concerns Environment declared-state source and connection settings:
transferring configuration control between Hubs, read-only mode for Environments
with a mirrored declared state source, restricting Environment actions by
reported-state source, selecting connection settings during Environment
creation, and displaying export/import source information. Conduit implements
this area (`internal/environments/domain/federation.go`), and it is the same
area as **PH-5**, where the public page has since added a fifth connection
setting Conduit does not have. That two independent records both point here is
worth noting; it is the part of the contract moving fastest.

## The Rosetta-only notes, read against the implementation

Done 2026-08-15. The notes were re-fetched and read live; none of their prose is
in this repository, and what follows is the conclusion rather than the text.

**The count moved, which is itself the finding about method.** The capture
recorded 58 Rosetta notes on dates the public page did not carry. Re-computing
it the same day against a live fetch of `core/whats-new` gives **55**, because
the public page now carries 229 distinct dates rather than the number it carried
at capture time. The divergence is a property of two moving records, not a fixed
quantity, so it should be recomputed when used and never quoted from memory.

**49 of the 55 are UI-surface notes and carry no contract for Conduit.**

| Surface | Notes | What they are |
| --- | ---: | --- |
| `autopilot` | 38 | The Apollo web console — tables, banners, filters, popovers |
| `apollo-build-factory-app` | 4 | Console panels and Hub settings tabs |
| `noc-inbox` | 3 | Notification inbox rendering |
| `apollo-ui-docs`, `hyperion-downdraft-form` | 4 | Dialogs and in-console documentation |
| **`apollo-cli`** | **6** | **The contract-bearing set** |

Conduit is a control plane and a command line; it does not reimplement Apollo's
console. A note that moves a histogram or collapses an overflow list states
nothing Conduit could conform to. This is not a dismissal of 49 notes — it is
the reason the tenant record's *size* advantage over the public page is smaller
than it looks for our purposes, and it is worth knowing before anyone mines it
again expecting 58 gaps.

**The six CLI notes, each checked:**

| Version | Note | Conduit |
| --- | --- | --- |
| `v0.463.0` | Contact team optional on `environment get` | **Divergence, recorded below** |
| `v0.500.0` | Space ID in `configure`, preferred over inferred | Native explicit profile/flag selection under ADR 0014; conflicting immutable assignments fail rather than move |
| `v0.518.0` | `--ignore-recalls` on module install/update | **Deliberate divergence, below** |
| `v0.519.0` | `--ignore-secret-requirements` on install/update | **Deliberate divergence, below** |
| `v0.524.0` | Improved help text on publish commands | No contract |
| `v0.531.0` | Space ID across commands, for guest users | Native Environment listing and Helm/Terraform publication use explicit selection plus existing grants; no primary-organization inference (ADR 0014) |

**Contact teams. Closed 2026-08-17; the paragraph below was true when written
and is now stale.** It read: *"Conduit's `newEnvironmentDescriptor` rejects an
empty contact team outright, so Conduit cannot represent such an Environment at
all."* It does not. `internal/conduitcli/workflow.go` now treats an empty
contact team as a first-class state, with the reasoning in a comment there and
coverage in `workflow_test.go` and `profile_test.go`. That shipped as #24.

The original finding stands as a finding: Apollo made the flag optional so that
`environment get` works for Environments with no contact team, which means
Apollo's model permits one, and Conduit's authoring path was narrower than its
own profile model — `ContactTeams` returned the empty set and
`validateReferences` accepted it all along. It would have surfaced as a failure
to import rather than as a missing feature.

**The two override flags are a genuine architectural divergence, not a gap.**
`--ignore-recalls` and `--ignore-secret-requirements` let an operator push an
installation past a blocking condition from the command line. Conduit's
equivalent path does not install: `entity install` proposes a desired state for
approval, so overriding a recall is a governance decision that belongs in the
change request, not a flag that bypasses it. Adding either flag verbatim would
put an unreviewed override on the command line of a system whose whole point is
that the override is reviewed. Recorded here so the next reader does not file it
as parity work.

**Two of the six are Space, which widens #17 rather than adding to it.** Both
CLI notes make Space a first-class argument to publishing commands, and one
extends it to guest users outside the owning organization. That is independent
corroboration that Space is load-bearing in Apollo's model, from a second record
— and #17 already gates five declared GraphQL coordinates and issue #16. It does
not change the decision; it raises its cost.

**The federation cluster corroborates #15 and does not widen it.** The 2025-08
and 2025-09 notes on declared-state source, edit-source transfer between Hubs,
read-only mirrored Environments, and export/import source display describe the
area `internal/environments/domain/federation.go` implements. One is directly
adjacent to #15: connection settings became selectable *during Environment
creation*. The open question there is still what an intermittent Environment
does, and none of these notes answers it.

## What is committed, and what is not

`observed/apollo-rosetta-release-notes.json` is a facts-only registry: note id,
date, type, release version, application identity, media count, and a SHA-256
over the whole record.

Not committed: `title`, `description`, `longDescription` — Palantir's prose —
and the tenant origin, which is recorded only as a salted SHA-256 so the
artifact states *which* tenant answered without stating the tenant. The
per-record digest means a reworded note is still detectable without the wording
being retained.

`scripts/verify-rosetta-release-notes.mjs` enforces all of that offline: it
fails on a prose field, on a hostname anywhere in the file, on a summary count
that disagrees with its own rows, on rows out of (date, id) order, and on a row
with no identity. "Facts only" is easy to state and easy to erode — the next
person extending the capture reaches for `title` because it makes the file
readable — so it is enforced rather than remembered.

## Re-capturing

```sh
FOUNDRY_HOST=https://<tenant> PALANTIR_TOKEN=... \
  node scripts/capture-rosetta-release-notes.mjs
```

It skips cleanly without credentials, because it reaches an authenticated tenant
and cannot run in ordinary CI. The verifier does run there, against the
committed file.

Two things about the service that are not guessable and cost hours if assumed,
recorded here because they are the kind of fact a second attempt rediscovers:

- **Auth is per-endpoint.** Pagination takes `Authorization: Bearer`; the media
  endpoint takes a `PALANTIR_TOKEN` cookie, because media is served to an
  `<img src>` that cannot set a header. The wrong channel returns
  `Conjure:MissingCredentials` — "no credentials seen" — which is a 401 and
  looks identical to `Default:Unauthorized` — "credentials seen and rejected".
  They mean opposite things.
- **`productId` enumerates.** `FOUNDRY`, `GOTHAM` and `APOLLO` answer; anything
  else returns `Rosetta:InvalidReleaseNoteProductId`, so the valid set comes
  from the service's own error rather than from guessing.

Media is not re-captured here. The 11 APOLLO media files are already durable,
pinned by digest in the palantir corpus and projected into
[`ui/evidence/APOLLO-ROSETTA-SOURCE-PROJECTION.json`](../ui/evidence/APOLLO-ROSETTA-SOURCE-PROJECTION.json);
see [`ui/APOLLO-ROSETTA-FEATURE-INVENTORY-2026-08-09.md`](../ui/APOLLO-ROSETTA-FEATURE-INVENTORY-2026-08-09.md).
That inventory recorded 169 notes on 2026-08-09; this capture finds 178, all
dated 2026-07-22 or earlier, so the nine are backfill into the record rather
than new releases.
