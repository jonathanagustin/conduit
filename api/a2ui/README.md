# A2UI 1.0 contract boundary

This package retains the primary A2UI 1.0 schemas at
`a2ui-project/a2ui@3ed9601862ecc78027e20ac80ad8b1b98a5d9079`, with exact
source URLs, SHA-256 hashes and its Apache-2.0 license in `upstream/manifest.json`.
No upstream renderer, SDK, runtime or UI component implementation is vendored.

`NewValidator` takes an application-owned catalog, validates its definition,
and compiles both agent-to-renderer and renderer-to-agent envelopes with the
existing Go JSON Schema engine. Resources resolve only to retained schemas,
the supplied catalog and built-in JSON Schema metaschemas. External loading is
disabled. The existing strict JSON decoder bounds input size/depth/numbers and
rejects duplicate keys and trailing values before validation.

The native profile narrows extension metadata keys to ASCII identifiers
`[A-Za-z_][A-Za-z0-9_]*`. The primary schema uses Unicode UAX #31 XID properties,
which Go's regular-expression engine does not implement. The exact known
pattern is mapped to this strict subset; other expressions still use the
ordinary engine and fail explicitly if unsupported. Non-ASCII extension keys
are deliberately rejected. Retained upstream bytes remain unchanged. This is
a disclosed Conduit restriction, not full Unicode protocol conformance.

Schema validation grants no operation. The caller must separately enforce the
approved catalog ID, component/function allowlist, surface lifetime, safe data
binding, HTML escaping, bounded validation functions, human activation and
native action authorization. Other catalogs, renderer-side function RPC and
agent-supplied code are not implicitly admitted. This foundation does not mount
a route, render a form, accept a plan or publish a delivery operation. The
renderer and governed AG-UI integration remain tracked in #641.

Tests exercise owned envelopes and catalog components, invalid/unknown members,
required action fields, duplicate keys, external references, extension-key
restrictions and every retained source checksum. These are contract fixtures,
not live-provider or tenant observations.
