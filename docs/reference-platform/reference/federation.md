# Hub topology and Environment ownership

Reviewed 2026-08-02 UTC from the public Environment connection and edit-source
pages captured as snapshots `c09ae8a0...a4a8b8` and
`0af1e357...388160`. These are reconstructed semantics over Platform's native
desired state and Bundle format, not private Hub protocol compatibility.

## Derived contract

- `AGENTS_CONNECTED` is the only state that accepts direct Agent work and
  issues Plans. `NO_CONNECTION_EXPECTED` and `RELAYED_FROM_ANOTHER_HUB` do not
  issue Plans; only agents-connected and relayed Environments participate in
  promotion because only those states hold reported health.
- A reviewed generation in a non-executing connection state is stored as a
  `RECORDED` desired-state record with no executable steps. Recording it cancels
  pending, approved, leased, and running Plans from the former writer so the
  Agent observes cancellation and rolls back.
- An Environment selection carries the exact signed reported state as an
  `ENVIRONMENT_REPORT` Bundle resource. Only a target explicitly configured as
  relayed admits it; the original Agent/Plan/timestamp attribution and the
  supplying Bundle/signing-key attribution are both retained.
- `EDIT_ON_THIS_HUB` owns settings locally and rejects an incoming Bundle that
  would overwrite them. `EDIT_ON_ANOTHER_HUB` admits upstream settings and
  allows per-Entity break glass; `COPY_OF_ANOTHER_HUB` admits upstream settings
  but remains read-only, including Entity config.
- Canary filters define the evidence set before topology admission is checked.
  An excluded disconnected Environment cannot block a canary stage it is not
  part of.

## Implementation boundary

Plan recording/creation/leasing, cooperative topology-transition cancellation,
promotion evidence, settings mutation, settings Bundle admission, and relayed
reported-state transport are gated and tested. Platform preserves these
semantics in its native signed Bundle; it does not claim Apollo's private Bundle
or Hub protocol.
