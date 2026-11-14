# Source-derived Foundry Conjure Go contract

Reviewed 2026-08-02. Platform's Foundry interoperability wire model is now a
generated artifact rather than a hand-maintained transcription of recovered
Java objects.

## Ground-truth source and evidence grain

The source is Palantir's official `@osdk/client.unstable` `2.43.0`
publication and its provenance-linked source commit
[`palantir/osdk-ts@c40ca076`](https://github.com/palantir/osdk-ts/tree/c40ca076c1459019a9327d79cf0581b6daf978a4/packages/client.unstable/src/generated/marketplace).
The npm registry reports SLSA provenance for that package resolving to the
same commit. The packed tarball has SHA-256
`a892d51988c83c9008aedf0e74ccf1ead8d5c808cb53c615b54d290e2817c02e`.
The four load-bearing generated TypeScript component files and hashes are
recorded in
[`osdk-client-unstable-2.43.0-conjure-evidence.json`](osdk-client-unstable-2.43.0-conjure-evidence.json).

This is primary evidence for the published client type declarations, field
names, optionality, enum values, and tagged-union variants. The original Maven
Conjure IR used to generate Marketplace TypeScript is not checked into the
public repository. The bounded Conjure IR in this repository is therefore a
derived normalization, not a claim that Palantir published that exact IR file
or package namespace. It proves no private Apollo Hub-to-Agent service,
authorization behavior, or server implementation.

## Repository arrangement

- [`api/conjure/foundry-apollo.selection.json`](../../api/conjure/foundry-apollo.selection.json)
  is the reviewable admission policy. It owns source coordinates, contract
  version, adapter/mode identifiers, output locations, the root type set, the
  deliberately opaque boundary, and the 42-file recovered-Java inventory.
- [`api/conjure/foundry-apollo.conjure.json`](../../api/conjure/foundry-apollo.conjure.json)
  is the 59-type derived IR: 29 aliases, two enums, 25 objects, and three
  unions. Its current SHA-256 is
  `a4b6e72d4be8e2b4f8e4ad178b33681c3c51477d708dee54294387e34713a11c`.
- [`scripts/osdk-conjure-contract.mjs`](../../scripts/osdk-conjure-contract.mjs)
  resolves TypeScript imports and discriminated unions from the pinned source.
  Unlike the earlier Factory prototype, it does not flatten the three admitted
  unions to `any`.
- [`internal/tools/conjuregen`](../../internal/tools/conjuregen) is a local,
  type-only IR generator. It rejects services and errors, emits idiomatic Go
  initialisms, and maps `ANY` to `json.RawMessage` as an explicit Platform
  boundary choice.
- [`internal/foundryinterop/wire`](../../internal/foundryinterop/wire) contains
  generated aliases, enums, objects, unions, and source constants. The stable
  package path is independent of contract version.
- [`internal/foundryinterop`](../../internal/foundryinterop) remains the
  handwritten hexagonal adapter. Generated transport types do not authorize,
  approve, promote, or execute work.

The upstream `V0` suffixes remain because they are source type names. Folder
paths and generator code do not embed that version. Contract version,
adapter ID, mode IDs, IR path, output path, and Go package are data in the
selection file. This preserves the existing `foundry-apollo-v0` ingress ID
without making the next source refresh require directory renames.

## Reproduction and gates

`./scripts/refresh-foundry-interop.sh` clones only the pinned source commit,
installs the pinned TypeScript parser in a temporary directory, derives the
bounded IR, and regenerates Go. It never writes credentials or vendor
dependencies into the repository.

`./scripts/verify-foundry-interop.sh` is the offline commit gate. It generates
Go from the committed IR into a temporary directory, requires a byte-clean
diff, and validates the configured source/version/inventory invariants. CI
runs this beside the public Apollo contract validators.

The generated unions retain raw JSON at their roots for unknown-variant
round-tripping, while typed known envelopes support the adapter. Unknown
secret variants still fail closed when translated into Platform's native
secret requirements.
