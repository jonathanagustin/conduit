# Apollo Platform Demo bundle evidence

This is the canonical, bounded record of a read-only review of
`/workspaces/factory/Apollo Platform Demo bundle.zip`. The proprietary source
media and extracted frames remain outside Git. This record documents semantic
evidence; it is not a transcript, a UI specification, or evidence of private
Apollo protocol compatibility.

## Integrity and method

The archive was inspected on 2026-08-01 UTC and passed `unzip -t`.

| Item | Properties | SHA-256 |
| --- | --- | --- |
| ZIP | 42,527,619 bytes; 267 entries; 50,505,809 uncompressed bytes | `0484b044c36e29add0e288af49ca2a89d360dc065535612eedf281ac55acc465` |
| MP4 | 14,448,357 bytes; about 264 seconds; 1920x1080 H.264/AAC | `d32c78726c3a8ac0a13249615fe4a8c34fa88c8b726c7d86be524a49b3e16510` |
| WebVTT transcript | 8,079 bytes; 93 timed cues | `1353b9d9693511e08be9fb041a82b6e8e051fe46110a3339f0c9fde87beb293a` |
| Text transcript | 5,015 bytes; 93 lines; 799 words | `58c8473f45961bb9f3d95d8a4e266dfc9f6f7d9be582a726aa5b17594750459b` |
| JPEG frames | `frame_0001.jpg` through `frame_0264.jpg`; 1920x1080; approximately one per second | Representative hashes below |

The ZIP uses Windows-style backslashes in entry names. Review used the video,
both transcripts, nine contact sheets spanning all 264 frames, and selected
full-resolution frames. The transcript appears machine-generated, so visible
UI state takes precedence where wording conflicts.

## Timeline ledger

Frame numbers are approximate seconds and make every observation below easy to
locate without committing the underlying copyrighted images.

| Frames | View / workflow | Material evidence |
| --- | --- | --- |
| 0001-0049 | Home and guided concepts | Product, Release, Environment, Installation, and Team are distinct concepts. Teams own Products or Environments; installation connects a Release to an Environment. |
| 0050-0120 | Environments and an Environment's installs | Six Environments appear in one namespace. The selected production Environment has Installs, Events, Config, Changes, Suppressions, and Plans. Fleet filtering treats health, rollout, and configuration as independent axes. Recalled installs can remain healthy or have indeterminate health. |
| 0121-0161 | Redis Product | Product views expose Releases, Installs, Remote Installs, and Configs. Release rows separately show type, recall, endorsement, installations, remote installations, migrations, and age. A promotion-pipeline area describes adjudication through release channels. |
| 0162-0170 | Home | The overview reports six Environments, eighteen Products, and the current team. These are notional demo counts, not scale claims. |
| 0171-0207 | Settings and Teams | Settings separates profile, namespace/environment, team/group, and release-channel administration. Five teams and six groups are shown. Team membership drives roles; Product and Environment screens expose owner/co-owner roles. |
| 0208-0255 | Install Map | Each installation is a selectable fleet cell. The view filters by Team, Product, and Environment; groups by Environment ID; colors by rollout status; and supports bulk Suppress, Mute, and additional actions. The demonstrated fleet has 55 up-to-date, one in grace, zero blocked, and seven recalled installs. |
| 0256-0264 | Home / close | No additional operational workflow is demonstrated. |

## Anchored frame observations

These hashes identify the exact evidence used for detailed claims. They are not
a complete frame manifest; the sequential inventory and archive hash protect
the complete reviewed input.

| Frame | SHA-256 | Directly visible evidence |
| --- | --- | --- |
| 0055 | `9bf9a36454b2c80a8096c4bccec401a96a6d4823a7ee72dce07c7c9f0ae6c42e` | All Environments: one namespace, six Environments, per-Environment install counts, and namespace grouping. |
| 0075 | `ee9b836ed6277b0996bb8c1a14302a4af4ef5cdfc534fd284cfce8b65010485a` | A production Environment with 15 installs; independent Health, Rollout, and Config columns; lifecycle, owner, channel, and status filters; failed-canary summary and bulk actions. |
| 0105 | `fc46ab6ed853e9812578887e12b47858a46ab0746ab260003684c36ce79b4659` | Two recalled installs whose health differs, proving recall is not a health state. |
| 0130 | `59f9f4ecc31a9c4e2c6f15860c3e1be5254e753167c489f81f681e7978cf5e26` | Redis installation version and owner; Product owner/co-owner roles; separate local and remote install navigation. |
| 0145 | `11d3c147f85c1a42a8b932ba8f92870d7d2a61d0b06b5c475a2c380f667bb4f7` | Release inventory with two recalled versions, installation counts, and promotion pipeline controls. |
| 0175 | `ff41cebf3b0d12489652249d878a4ae7c7eb7fc37f0e127719d98ccc6cfc52f7` | Team/group administration, create/edit/delete actions, and role assignment language. |
| 0235 | `01a93c62814cdffcd088ad51abc1417b9dade239781aac398aa49dab0e6553f3` | Fleet projection grouped by Environment and colored by rollout status, with status totals. |
| 0250 | `6d20e8f6f2613077717f5f0030dc122e595465fa25cad974bf80aa14c012ec6d` | Selecting one install exposes Suppress Installs, Mute Installs, and more bulk actions. |

## Semantic conclusions

The evidence supports the following implementation requirements:

1. Store Product, immutable Release, Environment, Installation/Entity, and
   Team ownership as separate identities and relationships.
2. Report desired Release, observed Release, health, rollout, configuration,
   lifecycle, and observation freshness independently. A single status field
   cannot reproduce the demonstrated semantics.
3. Model Release recall separately from installation suppression. Both need
   scoped actor, reason, time, and audit history, and both must be re-evaluated
   before execution.
4. Derive Product, Environment, and fleet views from the same canonical
   installation records. Expand a bulk operation into individually authorized,
   auditable changes.
5. Keep Product-owner and Environment-owner authority distinct. External
   identity/group systems may supply membership, but the delivery plane still
   needs resource-scoped authorization decisions and audit evidence.
6. Treat release channels and promotion as constraint evaluation that resolves
   to exact immutable releases, not mutable artifact tags.

## Coverage consequences

The two gaps originally exposed by the demo are now executable contracts:
reported state preserves independent health, rollout, configuration,
lifecycle, and freshness axes; fleet queries are resource-filtered and bulk
operations create individually authorized, audited, resumable changes.

Flux remains the Kubernetes desired-state reconciler. It can apply immutable
artifacts and report Kubernetes reconciliation, but it does not replace the
Product/Release eligibility, approval, recall, suppression, ownership,
promotion, or delivery-audit control plane established by this evidence.

## Explicit limits

The 2022 copyright footer says the data is notional and the material is
informational. The archive's 2026 timestamps do not establish the capture or
product version date. The tour does not execute install, approval, rollback,
recall, config update, or authorization denial end to end. It does not expose
Hub/Agent protocols, APIs, persistence, Kubernetes objects, availability,
failure semantics, disconnected delivery, or scale characteristics. Several
workflows are explicitly deferred to other videos.

Accordingly, this note is comprehensive for the semantic information recovered
from this bundle at one-frame-per-second resolution, but it does not claim to
preserve every pixel, spoken word, transient animation, or undisclosed product
behavior.
