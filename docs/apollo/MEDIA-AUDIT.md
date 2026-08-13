# Public Apollo video audit

## Capture result

The 2026-08-02 rendered-page crawl inspected all 241 URLs in
[`public-documentation-manifest.tsv`](public-documentation-manifest.tsv) for
YouTube, Vimeo, Wistia, MP4, WebM, MOV, and HLS references. It found one video:

| Page | Video | Placement |
| --- | --- | --- |
| [Welcome to Apollo](https://www.palantir.com/docs/apollo/apollo-getting-started/introduction-welcome/) | [Palantir Apollo 2023 — Mission Control for Software Deployment](https://www.youtube.com/watch?v=XmLVuZKd2L0) | Rendered page embed; not present in the page's source Markdown |

The inventory is reproducible with:

```text
node docs/apollo/audit-public-documentation-media.mjs \
  docs/apollo/public-documentation-manifest.tsv OUTPUT_JSON
```

The output contains source URLs and metadata only. Video, audio, captions, and
frame bodies are not committed.

## Local evidence and frame review

YouTube denied the initial unauthenticated `yt-dlp` stream request with its
automated-traffic challenge. The operator then supplied the downloaded MP4 for
direct local review. It is 271.601 seconds of 1902×1080 H.264 video with stereo
Opus audio and has SHA-256
`672167ff253ced15281ba751ab962c889e67638ccdcc678f5f332b5ea70e50a1`.

Twenty-eight frames at ten-second intervals from 00:00 through 04:30 were
extracted and visually inspected, in addition to the four public thumbnails.
The full-duration sequence shows:

- the Hub/Agent topology animation and its report/change directions;
- the Apollo overview and managed-environment inventory;
- Grafana's DEV → STAGING → PRODUCTION promotion pipeline, contributing Entity
  health, soak progress, adjudication constraints, and channel promotion;
- the Product-side constraint inventory including labels, dependencies,
  vulnerability/security evidence, recalls, and maintenance windows;
- the Environment-side release-channel and maintenance requirements; and
- an Environment change history followed by the closing title card.

These frames corroborate the architecture, Product/Release/Channel, promotion,
Environment-constraint, and audit contracts already indexed in the capability
map. They expose no private Hub-to-Agent wire format.

## Spoken-content transcript

The supplied audio was transcribed locally with `faster-whisper` 1.2.1 and the
English `base.en` model, then checked against the reviewed frames. The 55-line,
762-word timestamp output covers speech from 00:02.860 through 04:21.620 and has
SHA-256 `15f9a15cca5d0073522eaa5628d4851f6ea140d6d215fd9611eaee2c452731b2`.
The transcript body remains an uncommitted audit artifact. This is its compact
paraphrase:

| Time | Contract-bearing content |
| --- | --- |
| 00:03–00:41 | Apollo is a central delivery platform. A Hub is the control plane; an Agent in each managed Kubernetes cluster or Environment reports status and executes Hub-recommended changes. |
| 00:41–01:16 | Managed Environments run Product Releases such as Helm charts. Application owners publish to Release Channels and Environment owners subscribe according to risk tolerance. |
| 01:16–01:50 | A configurable DEV → STAGING → PRODUCTION promotion pipeline is illustrated with a Grafana Release. DEV subscribers receive a new Release first. |
| 01:50–02:27 | Health gathered from installed Environments, soak time, and configured adjudication thresholds determine automatic channel promotion. |
| 02:27–02:58 | Promotion is one part of constraint-based deployment; labels, dependencies, vulnerabilities, recalls, and maintenance windows also gate an upgrade recommendation. |
| 02:58–03:46 | Environment owners add release-channel and maintenance requirements. An upgrade is eligible only when the Product-owner and Environment-owner requirements intersect successfully. |
| 03:46–04:22 | Constraints are evaluated continuously. Autonomous execution remains inspectable through an auditable Environment change history, balancing stability and velocity. |

## Disposition

The direct frame and voice review adds corroborating explanation, not a new
protocol or domain state. Its architecture, outbound Agent
reporting/execution, promotion evaluation, constraint intersection, and
auditability are covered by executable tests. The local source, frames, audio,
and transcript were used only as temporary audit evidence and are not committed.
