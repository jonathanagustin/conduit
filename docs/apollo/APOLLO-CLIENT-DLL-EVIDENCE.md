# `apollo-client-dll` frontend asset evidence

Captured 2026-08-02 from the authenticated tenant's content-addressable
frontend store. The binary is not committed; this receipt preserves the exact
identity and bounded findings.

| Field | Value |
| --- | --- |
| Static asset label | `foundry-frontend-workshop/6.511.5/default/asset/apollo-client-dll` |
| CAS object | `frontend/2804ee8a6a0c9f5cc6b711cc345421adfaac324cb89a69f7a2648583e9f0026c.js` |
| Bytes | 304,341 |
| Download SHA-256 | `2804ee8a6a0c9f5cc6b711cc345421adfaac324cb89a69f7a2648583e9f0026c` |
| Integrity result | The downloaded bytes exactly match the CAS identifier |
| Authentication | Existing authorized bearer capture; token not retained |

## Findings

The bundle is the open-source Apollo **GraphQL** client, not a Palantir Apollo
software-delivery client. Its exported surface includes `ApolloClient`,
`InMemoryCache`, `ApolloLink`, `HttpLink`, `createHttpLink`, and
`ObservableQuery`. The bundle identifies Apollo Client `4.1.2`, GraphQL
`16.7.0`, and the library name `@apollo/client`, then installs its exports as
`window.foundryApolloClientDll`.

No application-named query, mutation, subscription, private Hub route, Spoke
lease operation, or deployment schema was found. The only named GraphQL query
is the standard `IntrospectionQuery` embedded by the GraphQL library.

## Consequence

The asset is primary evidence that this Workshop frontend version loads a
shared Apollo GraphQL client library. It is **not** evidence that Palantir's
Apollo deployment product uses this JavaScript bundle, and it does not unblock
H38's missing Apollo deployment GraphQL schema. The shared word “Apollo” is a
name collision that must not merge the two evidence tracks. The bounded method
and exact authorization input needed to recover the deployment schema are
recorded in [`reference/graphql-api.md`](reference/graphql-api.md).
