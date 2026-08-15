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
| Apollo CLI versions named | 42 | **134** |
| Distinct dates | 120 | 116 |

**93 CLI versions appear in Rosetta and never on the public page.** One appears
publicly and not in Rosetta. 58 Rosetta notes fall on dates the public page has
no entry for at all, and 46 public dates carry no Rosetta note — so neither is a
superset, but the tenant record is substantially the larger of the two for
version coverage.

The newest note on both sides is dated 2026-07-22, and the top of each record
agrees closely: the same CLI versions, the same subjects. They diverge going
backwards. The public page is an edited view, not a mirror, and a repository
that reads only the public page is reading the edit.

## What the divergence contains

The Rosetta-only notes are concentrated in two places the public page barely
covers.

**Application identities.** The public changelog speaks only of "Apollo" and
`apollo-cli`. Rosetta attributes notes to the shipping surfaces —
`autopilot@4.x`, `apollo-build-factory-app@0.x`, `noc-inbox@0.x` — which is how
a UI change is distinguished from a control-plane one.

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

Establishing which of those notes describe behaviour Conduit lacks needs the
notes read against the implementation one by one, which has not been done. This
document records the divergence and where it concentrates, not a gap list.

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
