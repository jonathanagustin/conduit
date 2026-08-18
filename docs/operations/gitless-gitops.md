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
| `GITLESS_RESOLUTION_LOG_KIND` | no | `file` (the compatibility default) or `postgres`. Multi-replica Hubs must use `postgres`. |
| `GITLESS_RESOLUTION_LOG_FILE` | with `file` | Where resolutions are appended before deployment. No default path. |
| `GITLESS_CONVERGENCE` | yes | `flux` or `conduit`. **No default** — see below. |
| `GITLESS_INTERVAL` | no | How often desired state is restated. Defaults to `5m`. |
| `GITLESS_COSIGN_BINARY` | no | The verifier. Empty resolves `cosign` on `PATH`. |
| `GITLESS_REGISTRY_CONFIG` | for private registries | Mounted Docker `config.json` used by source resolution, Cosign verification, and Flux-manifest publication. The file **must be named `config.json`** — see below. It must contain a usable entry for every registry host reached; there is no anonymous fallback for a missing host. |
| `GITLESS_REGISTRY_TOKEN_REALMS` | for registries whose token realm is on another host | Comma-separated `registry-host=realm-host`. Unset, a registry may direct credentials only at itself. Needed for Docker Hub and the GitLab registry — see below. |
| `GITLESS_FLUX_NAMESPACE` | with `flux` | Namespace the rendered manifests declare. Defaults to `flux-system`. |
| `GITLESS_FLUX_SERVICE_ACCOUNT` | with `flux` | The bounded identity the Kustomization reconciles as. **No default.** |
| `GITLESS_FLUX_PULL_SECRET` | with `flux` | The read-only registry pull Secret. **No default.** |
| `GITLESS_FLUX_PATH` | with `flux` | Path inside the artifact. Defaults to `./runtime`. |
| `GITLESS_FLUX_MANIFEST_REPOSITORY` | with `flux` | Registry repository the rendered manifests are published to, one tag per environment. **No default.** |
| `GITLESS_FLUX_INSECURE_REGISTRY` | no | `true` sends http rather than https. Explicit rather than inferred from the host, so it cannot be switched on by a hostname somebody chose. |

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

### Private registry credentials

Mount one read-only Docker `config.json` and set `GITLESS_REGISTRY_CONFIG` to
its path. A single file may contain separate entries for the Foundry source
registry and the manifest repository. The Hub uses the decoded credentials
through the resolver and publisher ports, and points Cosign at the same file.
Both OCI clients answer Basic and Bearer registry challenges; they do not send
credentials to a host before it challenges. Errors name the registry host but
never the username, password, or encoded `auth` value.

Once this variable is set, a registry absent from the file is a configuration
error instead of an anonymous retry. Leave it unset only when both registries
are intentionally public. The Hub image carries the same Cosign v3.1.3 command
used by the signing workflows and Gitless acceptance suite, rebuilt from its
locked module graph under the pinned Go toolchain, so no runtime download or
mutable tools volume is required.

#### The file must be named `config.json`

`cosign verify` has **no** `--registry-config` flag. What it reads is the
directory named by `DOCKER_CONFIG`, onto which it joins `config.json`. So the
Hub exports `DOCKER_CONFIG=$(dirname "$GITLESS_REGISTRY_CONFIG")` to the Cosign
child process, and neither the path nor the credential bytes reach `argv`.

That makes the file's *name* load-bearing, and getting it wrong has no symptom:
Cosign pointed at a directory with no `config.json` does not complain, it
authenticates anonymously — so resolution and publication would succeed with
credentials while verification failed with a bare 401. The Hub therefore refuses
to start when `GITLESS_REGISTRY_CONFIG` names anything else.

A Kubernetes `kubernetes.io/dockerconfigjson` Secret projects its data under the
key `.dockerconfigjson`, so project it explicitly:

```yaml
volumes:
  - name: registry-credentials
    secret:
      secretName: conduit-registry
      items:
        - key: .dockerconfigjson
          path: config.json
```

#### Where a credential may be sent

A registry that wants Bearer authentication answers `401` with a
`WWW-Authenticate` header naming a *realm* — the endpoint where credentials are
exchanged for a token. That header arrives on an **unauthenticated** response,
so it is chosen by whoever answered, not by you.

Conduit constrains the realm rather than believing it. A realm is followed only
when **all** of these hold:

- it is an absolute **https** URL — always, with no setting that relaxes it;
- it carries no userinfo of its own;
- its host (including port) is either the registry being contacted, or a host
  the operator declared for that registry in `GITLESS_REGISTRY_TOKEN_REALMS`.

Anything else is refused *before the credential is read*, so a registry cannot
direct a robot's password to a host of its choosing, and a refusal costs no
credential access at all.

`GITLESS_FLUX_INSECURE_REGISTRY=true` does **not** reach this. It says the
manifest registry is contacted over http — an air-gapped mirror terminating TLS
elsewhere. It is not a statement that a push credential may be posted in the
clear, and it is not read as one.

##### Registries whose token realm is on another host

Same-host is the common shape but not the only one. Measured against live
`GET /v2/` challenges:

| Registry | Realm | Same host |
| --- | --- | --- |
| `ghcr.io` | `https://ghcr.io/token` | yes |
| `quay.io` | `https://quay.io/v2/auth` | yes |
| `gcr.io` | `https://gcr.io/v2/token` | yes |
| `public.ecr.aws` | `https://public.ecr.aws/token/` | yes |
| `registry-1.docker.io` | `https://auth.docker.io/token` | **no** |
| `registry.gitlab.com` | `https://gitlab.com/jwt/auth` | **no** |

So a plain same-host rule would break Docker Hub and the GitLab container
registry. Declare those instead:

```bash
GITLESS_REGISTRY_TOKEN_REALMS="registry-1.docker.io=auth.docker.io,registry.gitlab.com=gitlab.com"
```

Entries are `registry-host=realm-host`, both bare hosts (a port is allowed, a
URL or a `*` is rejected). A declaration applies to **that registry only** —
declaring `auth.docker.io` for Docker Hub does not let some other registry
direct credentials there. Unset, every registry may direct credentials at itself
and nowhere else.

For comparison, go-containerregistry's `validateRealmURL`
(`pkg/v1/remote/transport/bearer.go`, added for its issue #2258) constrains the
scheme the same way, always permits the registry's own `host:port`, and for any
other host rejects only realms whose host is a private or link-local IP
*literal* — leaving DNS-named cross-host realms permitted, and DNS-based SSRF
explicitly out of scope. containerd's `core/remotes/docker/auth/fetch.go`
constrains nothing: it parses the realm and posts to it. Conduit is stricter
than both, because a Hub reaches a small set of registries an operator already
had to name.

### Where the manifests go

With `flux`, Conduit renders an `OCIRepository` and a `Kustomization` and
publishes them to `GITLESS_FLUX_MANIFEST_REPOSITORY` as an OCI artifact tagged
with the environment name. That artifact is what the cluster's Flux pulls —
which is the whole point: the manifests live in a registry, not in Git, and the
cluster needs no Git credential to reach them.

Republishing identical bytes does not re-upload them, so a five-minute reconcile
interval is not a five-minute upload loop.

"Which digest is this environment converging on" is answered by reading
`spec.ref.digest` out of the *published* manifests — not by the digest of the
artifact carrying them. Confusing the two would make every republish look like a
source change.

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

**`conduit` is not yet wired in the Hub binary, and the reason is a decision
rather than missing code.** Proposing desired state requires a principal holding
`change:propose` on the environment, and the scheduler is a background loop with
no request and no identity. Somebody has to decide who a scheduled source change
proposes as — a dedicated service principal, an operator-nominated one per
binding, or something else — and each answer changes who effectively holds
propose authority. Recorded as **PH-3**. Setting it fails at startup rather than
starting something that cannot work.

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

Set `GITLESS_RESOLUTION_LOG_KIND=postgres` on a multi-replica Hub. It reuses
`POSTGRES_DSN_FILE`, applies the `gitless-resolution-log` component's
forward-only migration, and appends every replica's records to one ordered
table. Startup fails if the Hub repository is not also PostgreSQL-backed; a
split durability profile would make the audit history look shared when it is
not.

The `file` profile remains for a deliberately single-replica deployment. It
stores one JSON object per line, appended with `O_APPEND` and fsynced before the
write is reported successful:

```json
{"recordedAt":"…","environment":"prod","selector":"ghcr.io/…:latest-stable",
 "selectorKind":"CHANNEL","repository":"ghcr.io/…","digest":"sha256:…",
 "superseded":"sha256:…","moved":true,"publisher":"https://github.com/…"}
```

**The file profile's bound:** one file on one filesystem. It is the wrong
adapter for several replicas; the PostgreSQL profile is the shared durable
store for that topology.

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
