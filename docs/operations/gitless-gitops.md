# Running Gitless GitOps

Conduit can follow a signed OCI artifact as an environment's desired state,
without any cluster holding a Git credential. This is how to turn it on, and
what each setting commits you to.

It is **off unless configured on**. A Hub that sets none of these starts exactly
as it did before the feature existed. That is deliberate: defaulting to some
source would mean an upgrade could begin deploying things nobody asked for.

## Configuration

| Variable | Required | Meaning |
| --- | --- | --- |
| `GITLESS_BINDINGS` | to enable | Comma-separated `environment=repository:channel` or `environment=repository@sha256:…`. Empty means off. |
| `GITLESS_PUBLISHER_POLICY_FILE` | yes | Which publisher each environment trusts. No default. |
| `GITLESS_RESOLUTION_LOG_FILE` | yes | Where resolutions are appended before deployment. No default. |
| `GITLESS_CONVERGENCE` | yes | `flux` or `conduit`. **No default** — see below. |
| `GITLESS_INTERVAL` | no | How often desired state is restated. Defaults to `5m`. |
| `GITLESS_COSIGN_BINARY` | no | The verifier. Empty resolves `cosign` on `PATH`. |
| `GITLESS_FLUX_NAMESPACE` | with `flux` | Namespace the rendered manifests declare. Defaults to `flux-system`. |
| `GITLESS_FLUX_SERVICE_ACCOUNT` | with `flux` | The bounded identity the Kustomization reconciles as. **No default.** |
| `GITLESS_FLUX_PULL_SECRET` | with `flux` | The read-only registry pull Secret. **No default.** |
| `GITLESS_FLUX_PATH` | with `flux` | Path inside the artifact. Defaults to `./runtime`. |

A misconfiguration is a **startup failure**, not a first-tick surprise. A Hub
that started cleanly and then silently never deployed one of its environments is
worse than one that refused to start, so every binding is parsed and every bound
environment's policy is looked up before the process begins serving.

### The publisher policy file

```json
{
  "environments": {
    "prod": {
      "certificateIssuer": "^https://token\\.actions\\.githubusercontent\\.com$",
      "certificateIdentity": "^https://github\\.com/OWNER/REPO/\\.github/workflows/platform-images\\.yml@refs/tags/v[0-9]+\\.[0-9]+\\.[0-9]+$"
    }
  }
}
```

Both patterns must be **anchored**. An unanchored identity is how a signer
policy becomes decoration: `github.com/example/conduit` matches
`github.com/attacker/github.com/example/conduit`. The file is rejected at load
rather than at first use.

An environment absent from this file is **refused**, not given a default. A
default publisher policy is one nobody reviewed, applied to the environment
somebody forgot — reliably the one that mattered.

Unknown fields are rejected. `certificateIdentiy` would otherwise leave an
environment trusting an empty pattern, and a setting somebody believed was doing
something (`"trustAnySigner": true`) would be read and ignored.

## Why convergence has no default

`GITLESS_CONVERGENCE` decides **who makes the environment match the admitted
bytes**, and the two answers have different governance:

- **`flux`** renders an `OCIRepository` and a `Kustomization` pinned to the
  admitted digest and lets Flux converge in-cluster. Fast, and it does not pass
  through Conduit's Plan and approval semantics.
- **`conduit`** hands the admitted artifact to Conduit's own desired state, which
  is compiled into a governed Plan and approved under Governance's rules. A
  source change becomes something somebody can be asked to approve rather than
  something a controller has already done.

Falling back into either would be choosing a governance model on an operator's
behalf, so neither is a default.

**`conduit` is not yet wired in the Hub binary.** Its ports need an artifact
fetcher and a desired-state store that reach into Delivery. Setting it fails at
startup rather than starting something that cannot work.

## What a channel does and does not do

A channel is the only mutable thing this design tolerates. It is resolved to a
digest, the resolution is **recorded before the deployment happens**, and only
the digest converges. Moving a tag cannot change what a cluster is already
running — a new digest has to be admitted and recorded first.

This is a deliberate divergence from the Flux D2 reference architecture, which
tracks `latest` on staging and `latest-stable` on production. ADR 0001 records
as a consequence that *moving a convenient tag cannot silently deploy different
bytes*, and that property is kept.

A **broken resolution log stops delivery.** That is chosen: a deployment nobody
can attribute afterwards is the state this whole design exists to prevent.

## Reading the logs

| Level | Event | Meaning |
| --- | --- | --- |
| `info` | `Gitless GitOps deployed` | Something converged. Carries the digest, the publisher, and what it superseded. |
| `debug` | `Gitless GitOps unchanged` | The common case on a short interval. |
| `warn` | `Gitless GitOps refused a source` | **The machinery worked.** Something was published that this environment does not trust, and it correctly did not deploy. |
| `error` | `Gitless GitOps could not reconcile` | Conduit could not find out — an unreachable registry, an unavailable verifier. This is the one that needs somebody. |

Refusals warn and failures error, which looks backwards and is not. Logging both
at `error` buries the case that needs attention in the case that is working.

## The resolution log

One JSON object per line, appended with `O_APPEND` and fsynced before the write
is reported successful:

```json
{"recordedAt":"…","environment":"prod","selector":"ghcr.io/…:latest-stable",
 "selectorKind":"CHANNEL","repository":"ghcr.io/…","digest":"sha256:…",
 "superseded":"sha256:…","moved":true,"publisher":"https://github.com/…"}
```

**Its bound:** one file on one filesystem. A Hub running several replicas needs a
shared durable store, and this adapter is then the wrong one — not
misconfigured, wrong. Nothing detects that at runtime; it is a deployment-time
decision.

## Verifying it

- `scripts/test-gitless-acceptance.sh` runs the whole loop against a real
  registry, a real keyless signature and a real Flux API server, and requires
  the same artifact to be refused under a policy naming a different publisher.
- `scripts/test-gitless-flux-manifests.sh` validates the rendered manifests
  against real Flux CRDs. It also records what it **cannot** cover: an API server
  accepts a manifest with verification and impersonation removed, so policy is
  guarded by the Go tests rather than by the cluster.

Both skip cleanly without a cluster, and both live in the manual-only
`Gitless OCI acceptance` workflow rather than the Trunk gate.

See ADR 0004 for why admission is a domain rule rather than a port.
