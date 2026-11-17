# ADR 0019: English console copy for the current release

Status: **Accepted** (2026-11-17), issue #827.

## Context

The shipped console declares `lang="en"` in `web/index.html`. Navigation,
action labels, permission reasons and recovery messages are English literals;
there is no locale selector or translated message catalog. The browser tests
exercise those English labels. A language declaration alone does not establish
translation or right-to-left support.

## Decision

The current release supports English console copy. Retain that complete
existing language path instead of advertising partially translated locales.
This is a Conduit-native support decision, not a claim about another product.

A future supported locale needs a reviewed message catalog, explicit fallback
behavior, an appropriate document language, and keyboard/accessibility and
interaction coverage for that locale. Translation must preserve the distinction
between refused access, missing data, unavailable services and recorded success.

This decision covers console chrome, authored messages and accessible names.
It does not translate user-authored content, immutable identifiers, API status
literals, resource coordinates or external provider messages. Existing
browser-native date/number formatting may follow local settings; this decision
does not establish a new formatting or API wire contract.

## Consequences

Operators need English for the currently supported console path. Additional
languages and right-to-left layouts are not declared supported. Future work can
extract copy into catalogs without changing IDs, authority decisions or stored
records. Snapshot and interaction tests retain the declared English fixture
language; they do not prove localization coverage.
