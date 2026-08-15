# Apollo GraphQL API recovery boundary

Reviewed 2026-08-03 against the official
[Apollo API Explorer documentation](https://www.palantir.com/docs/apollo/apollo-references/apollo-api-documentation/)
and an authorized, read-only capture of the Foundry **Connected Hubs** route.

## Public contract

Palantir documents that certain Apollo APIs are GraphQL and that the API
Explorer (GraphiQL) is exposed directly by an Apollo Hub. Its documentation
panel enumerates the queries and mutations accessible in that particular Hub.
The published examples establish at least these selections:

```graphql
query GetCurrentUser {
  me { id fullName }
}

query GetEnvironments {
  apollo {
    environments(pageSize: 100) {
      environments { id }
    }
  }
}
```

The documentation does not publish a universal Explorer pathname, endpoint,
downloadable SDL, authentication fixture, or complete schema. Consequently the
examples prove only their named fields and cannot support a broader
compatibility claim.

## Authorized discovery result

An authenticated capture of the current Foundry enrollment's
`/workspace/control-panel/connected-hubs` route completed without missing
artifacts at `2026-08-03T04:43:08.453Z`. Raw HTML, bundles, responses, and the
tenant origin remain outside this repository. The integrity receipt is:

| Artifact | Observation |
| --- | --- |
| Capture manifest | SHA-256 `8bef34e33677617ff6a922594a91a1b0b7249be892f6b993faff8d3e37ac99ee`; 231 requests and responses, 191/191 assets captured |
| Accessibility tree | SHA-256 `1e3c7a8f9df6bdcb916880c4360ee6c760116b5d5f502040e9ee6e31dc0ad8b6`; the Connected Hubs extension reports **Excluded from your plan** |
| GraphQL request shapes | Five Control Panel shell/enrollment operations; none is an Apollo Hub operation |
| Network result | No Apollo-, Hub-, connection-, or Marketplace-named service request beyond the Connected Hubs document route |

The plan gate prevents the extension from loading or listing connections. The
capture therefore exposes neither an Apollo Hub origin nor its API Explorer.
It is evidence of a blocked discovery path, not evidence that the enrollment
has no Apollo Hub connection.

## Recovery method once a Hub is authorized

The Factory/Palantir GraphQL recovery workflow is applicable with a strict
scope boundary:

1. Capture the authenticated Hub's Explorer and representative Apollo
   application routes, retaining downloaded bundles only in temporary storage.
2. Statically recover each shipped GraphQL operation together with its own
   transitive fragment dependency closure. Do not globally merge fragments by
   name because separate bundles may ship different same-named definitions.
3. Prefer standard introspection when the Hub permits it. If introspection is
   intentionally thin, use validation-only requests with a guaranteed-invalid
   guard to learn only exact field-output and argument-input types without
   executing the query or mutation.
4. Emit a deterministic **observed** SDL and fail unless it passes GraphQL
   schema validation and every captured operation plus exact closure validates
   against it.
5. Commit only scrubbed operation/type metadata, source hashes, completeness
   counts, and diagnostics. Never commit tenant origins, tokens, cookies,
   authenticated documents, response bodies, or private bundles.

This can close H38 only for the captured GraphQL contract. It cannot prove
unused private fields and does not establish the separate Hub-to-Spoke control
wire in H18. Generated Go clients, adapters, or resolvers must therefore be
labelled against the observed schema version/hash rather than as general
Palantir Apollo compatibility.

## Recovery result (2026-08-15)

An authorized Hub was reached and the method above was run. The result is
committed as `api/graphql/apollo.observed.graphql` with its recovery record and
completeness report under `docs/apollo/observed/`.

What the Hub established:

| Observation | Result |
| --- | --- |
| `query { me { id } }` | resolves |
| `query { apollo { environments(pageSize: 100) { environments { id } } } }` | resolves; the published shape is real |
| `Query.apollo` type name | `ApolloQuery` |
| Introspection | **thin by design** -- `__schema` returns 20 types and its `Query` advertises only `me`. It names no part of the Apollo surface. |
| Recovered | 3 types, 11 fields, 7 argument types, in 28 requests |
| Emitted to SDL | 5 fields. The other 7 are omitted because their return types were not established, and are listed in the completeness report. |

Because introspection is thin, the surface was recovered by validation probing:
a document carrying a guaranteed-invalid field fails validation as a whole, so
the Hub executes none of it while still reporting `FieldUndefined`,
`MissingFieldArgument`, `WrongType` and `SubSelectionRequired` for everything
else in the document. Only the return-type probe executes, selecting
`__typename` alone.

The guard is also a canary, and that is what makes the method fail closed.
"No error for this candidate" is evidence the candidate exists only if the Hub
actually validated the document; a malformed document returns a parse error and
no field errors, which reads exactly like "every candidate exists". The prober
therefore requires the guard's own error in every discovery response and aborts
otherwise. An earlier revision lacked that check, emitted an unbalanced
document, and reported fifteen invented fields on two types before the guard
was added.

## Bounds on this result

- It is one Hub on one date, answering one candidate set. A field nobody asked
  about is absent from the record, not from the Hub.
- Seven fields (`bundle`, `bundles`, `entity`, `environment`, `productRelease`,
  and the two page item lists) resolved but could not be typed: they require a
  real RID, or returned an empty list in this tenant, so no `__typename` was
  observable. They are recorded as recovered-but-untyped rather than guessed.
- No mutation type is reachable: `__schema.mutationType` is null.
- This establishes nothing about the private Hub-to-Agent control wire, which
  remains H9 and BLOCKED.
- Generated clients or adapters must be labelled against `schemaSha256` in the
  completeness report, not as general Palantir Apollo compatibility.
