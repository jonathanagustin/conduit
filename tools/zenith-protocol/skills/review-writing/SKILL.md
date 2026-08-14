---
name: review-writing
description: Detect clustered machine-writing patterns in prose and revise them into plain, specific language without claiming to identify authorship. Use when reviewing documentation, reports, decisions, or other shipped prose for AI tells, vague attribution, promotional language, repetitive structure, or unnatural cadence.
---

# Review writing

Treat pattern detection as an editing aid, never proof that a person or model
wrote the text.

## Run the review

1. Run `node scripts/review-writing.mjs <file> --json` from this skill directory.
2. Inspect every reported line in context. Discard quotations, required domain
   terms, and isolated matches that do not weaken the prose.
3. Report counts by category, specific lines, and `LOW`, `MEDIUM`, or `HIGH`
   pattern confidence. Say explicitly that the confidence describes pattern
   density, not authorship.
4. Revise the draft: state the claim directly, cite named sources, replace
   promotional words with measurements, repeat stable technical terms, and cut
   formulaic openings, endings, and parallel constructions.
5. Run the script again and review the diff. Do not optimize blindly for a zero
   score or distort legitimate language to satisfy the heuristic.

The detector measures vocabulary, phrases, em-dash density, and repeated
inline-header bullets. It emits structured evidence with line numbers and has no
network access. Short documents are marked as less reliable.

## Provenance

The pattern taxonomy and workflow are adapted from
[`ai-ecoverse/skills`' `ai-writing-detector`](https://github.com/ai-ecoverse/skills/tree/main/skills/ai-writing-detector),
Apache-2.0, reviewed at commit `2f4928276227f8d4a1e443ee9eb280148f1355d3`.
Zenith's script is a portable Node implementation; it does not require the
upstream `sliccy:` runtime. The upstream detector and this adaptation were
changed to emphasize revision and to avoid authorship claims.
