## Outcome

<!-- One observable, independently reversible change. -->

## Tenant fidelity

<!-- Complete one row per material Apollo-facing claim. Use Unknown rather than guessing. -->

| Claim or behavior | Class: Public contract / Tenant observation / Corroborating / Conduit extension / Unknown | Immutable source or sanitized evidence | Scope not established |
| --- | --- | --- | --- |
| | | | |

### Deliberate divergences

<!-- State every way Conduit extends or differs from the observed Apollo behavior. Write "None" only when the evidence above supports that statement. -->

## Contract and generated surfaces

- [ ] OpenAPI/Conjure is the transport authority where a published contract exists.
- [ ] Generated output and its pinned source changed together, or this change has no generated surface.
- [ ] Unknown behavior remains blocked or explicitly Conduit-native; no Apollo behavior was inferred.
- [ ] No raw tenant identifiers, URLs, payloads, credentials, cookies, tokens, binaries, or private source were committed.

## Verification

<!-- List focused commands and their results. -->

- [ ] Relevant focused tests passed locally.
- [ ] `./scripts/verify-local.sh` passed.
- [ ] The manual **Trunk gate** workflow passed for this exact commit.
- [ ] The branch contains one coherent change and is current with `main`.
