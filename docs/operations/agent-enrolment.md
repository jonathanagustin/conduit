# Agent enrolment, revocation and certificate expiry

Issue #847. An Agent authenticates to the Hub with a client certificate. This
page says what that certificate must contain, one way to issue it, how to cut
an Agent off, and how to see an expiry coming. Rotating the CA that signs these
certificates is in [key-rotation.md](key-rotation.md).

Every statement below is tied to the code that makes it true. What has not
been run on a cluster is marked **not exercised**, and there is a list of
those at the end.

## What the Hub requires of an Agent certificate

| Requirement | Where it is decided |
| --- | --- |
| Exactly this URI SAN: `spiffe://platform.delivery/environment/<environmentId>/agent/<agentId>` | `AgentIdentityFromCertificate`, `internal/identity/domain/identity.go` |
| The trust domain is the literal `platform.delivery`. It is not configurable. | `agentIdentityTrustDomain`, same file |
| `<environmentId>` and `<agentId>` are DNS labels | same function |
| The certificate chains to a root in the file `AGENT_CLIENT_CA_FILE` names | `AgentServerTLSConfig`, `internal/controlplane/server_tls.go` |
| The Environment in the SAN equals the Environment in the request path | each Agent handler, `internal/controlplane/http.go` |

The subject, the common name and DNS SANs are not read. An Agent is its URI
SAN and nothing else.

The Hub chart mounts the CA bundle from the ConfigMap `tls.externalCAConfigMapName`
(key `tls.externalCAKey`, default `ca.pem`) at `/etc/ssl/platform-ca/ca.pem`
and sets `AGENT_CLIENT_CA_FILE` to that path.

## What the Spoke chart requires

`deploy/helm/platform-spoke` mounts one Secret, `tls.clientSecretName`, at
`/run/platform-tls` and points the Agent at two keys in it:

| Secret key | Agent variable |
| --- | --- |
| `tls.crt` | `CONTROL_PLANE_CLIENT_CERT_FILE` |
| `tls.key` | `CONTROL_PLANE_CLIENT_KEY_FILE` |

That is the shape of a `kubernetes.io/tls` Secret, which is what cert-manager
writes. `environmentId` and `agentId` in the chart values must equal the two
segments of the SAN: the Agent asks for leases at
`/v1/environments/<environmentId>/leases`, and the Hub refuses a certificate
whose Environment differs from the path.

`tls.externalCAConfigMapName` on the Spoke is a different thing: the CA that
signed the **Hub's server** certificate, so the Agent can verify the Hub.

## Enrolment with cert-manager

**Not exercised on a cluster.** This repository installs no cert-manager and
has not applied these manifests. They are in `examples/agent-enrolment/`:

- `cert-manager-issuer.yaml` — a CA `Issuer` over a Secret holding the Agent
  CA's certificate and key.
- `cert-manager-certificate.yaml` — the Agent's `Certificate`.
- `spoke-values.yaml` — the Spoke chart values that consume it.

The part that matters is this, from the `Certificate`:

```yaml
spec:
  secretName: platform-agent-tls        # the Spoke chart's tls.clientSecretName
  privateKey:
    rotationPolicy: Always              # a new key on every issuance
  usages: ["client auth", "digital signature"]
  uris:
    - spiffe://platform.delivery/environment/prod-use2/agent/primary
```

and the matching Spoke values:

```yaml
environmentId: prod-use2
agentId: primary
tls:
  clientSecretName: platform-agent-tls
```

Steps:

1. Create the Agent CA and put its **certificate only** into the Hub's CA
   ConfigMap. Restart the Hub: it reads `AGENT_CLIENT_CA_FILE` once, at start.
2. Put the CA's certificate and key in the Secret the `Issuer` names. Whoever
   can read that Secret can mint an Agent identity for any Environment the Hub
   trusts that CA for. One CA per Environment limits that to one Environment.
3. Apply the `Issuer` and the `Certificate` in the namespace the Spoke chart is
   released into. Wait for the Secret to exist.
4. Install the Spoke chart with the values above.
5. Confirm: `conduit agent-credential list <environmentId>` shows the Agent,
   the serial it presented and when the certificate expires.

### Renewal needs a restart

The Agent loads its certificate once, when it starts
(`tls.LoadX509KeyPair` in `internal/agent/transport.go`). cert-manager renewing
the Secret does **not** change what a running Agent presents. Until the Agent
Pod restarts it keeps presenting the old certificate, and stops working when
that one expires.

Nothing in this repository restarts the Agent when the Secret changes. Either
restart the Deployment as part of renewal, or run something that does it for
you. The expiry metric below exists to catch the case where nobody did.

The Hub's SPIFFE Workload API mode (`SPIFFE_ENDPOINT_SOCKET` on both sides)
rotates certificates without a restart. It is a different deployment shape,
the charts do not configure it, and it is not covered here.

## Enrolment in a UDS deployment

[uds-package.md](uds-package.md) creates `platform-agent-tls` by hand with
`openssl` and `kubectl create secret`. The contract is the same Secret with the
same two keys, so the cert-manager `Certificate` above replaces those commands
if cert-manager is present in the cluster. **Not exercised:** no UDS bundle
here deploys cert-manager, and the UDS package was not installed with a
cert-manager-issued Agent certificate.

## Revoking an Agent

The Hub keeps a deny-list. A request on any Agent route is checked against it
before the route's handler runs (`guardAgentCredentials`,
`internal/hub/composition/agent_credentials.go`). Which routes are Agent routes
is read from the route register (`HubRouteAuthorization`), so a new Agent route
is covered without a change here.

There are three scopes.

| Scope | CLI | Refuses | Re-enrolment |
| --- | --- | --- | --- |
| `SERIAL` | `--serial <hex>` | That one certificate | Any other certificate for the Agent works at once |
| `AGENT` | `--agent-id <id>` | Every certificate for that Environment and Agent whose `NotBefore` is at or before the revocation's `notBefore` | A certificate issued after `notBefore` is admitted, with no second operator action |
| `ENVIRONMENT` | `--all-agents` | The same rule, for every Agent of the Environment | The same |

`notBefore` defaults to the moment of revocation. Pass `--not-before` to set it.

```sh
# The key for prod-use2/primary leaked. Cut it off now.
conduit agent-credential revoke prod-use2 --agent-id primary --reason "key leaked, INC-1234"

# One certificate only. The serial is in `agent-credential list`, or from
# `openssl x509 -noout -serial`.
conduit agent-credential revoke prod-use2 --serial 4F:A2:01 --reason "superseded"

# Everything in the Environment.
conduit agent-credential revoke prod-use2 --all-agents --reason "CA key exposure"

conduit agent-credential revocations prod-use2
conduit agent-credential unrevoke prod-use2 arv_0123456789abcdef0123456789abcdef
```

Revoking the same thing twice replaces the one record; it does not add a second.

### Three things to know before relying on it

**Re-enrol with a new private key.** An `AGENT` or `ENVIRONMENT` revocation
admits any certificate issued after its instant. A certificate reissued for the
leaked key readmits whoever holds that key. `rotationPolicy: Always` in the
example is what prevents this.

**The decision uses a timestamp the issuer wrote.** A CA that backdates
`NotBefore`, or whose clock is behind the Hub's, issues a replacement that is
still refused until the instant passes. If a fresh certificate is refused, read
its `NotBefore` and compare it with the revocation's `notBefore`.

**A revocation is not CA rotation.** If the CA key itself is exposed, revoke
the Environment to stop the bleeding, then rotate the CA
([key-rotation.md](key-rotation.md)).

### What a revoked Agent sees

`403` with `{"error":"Agent credential is revoked","revocationId":"arv_…"}` on
every Agent route.

### What happens to work the Agent was doing

A revoked Agent holding a lease cannot start, heartbeat or finish its Plan:
those are Agent routes and are refused like any other. The Plan stays `LEASED`
until the lease expires. After that the next admitted Agent of the Environment
that polls is handed the Plan as a new attempt. This is the existing
lease-expiry path in `Lifecycle` (`internal/controlplane/lifecycle.go`); no
lease behaviour was added for revocation.

Two consequences:

- The Plan is not failed or cancelled by the revocation. It waits out the
  lease (`leaseDuration` in the Spoke chart, 2 minutes by default).
- If the revoked Agent was the Environment's only Agent, nothing picks the
  Plan up until an admitted Agent polls.

Whatever the revoked Agent already did in the cluster is not undone. The next
attempt runs the Plan again.

### If the deny-list cannot be read

Every Agent request is answered `503` with `Retry-After` until it can be. An
Agent is never admitted because the database was down. Operator, public and
health routes do not consult the deny-list on the way in and keep working,
except the four routes on this page, which answer `503` themselves.

### Authority and audit

| Route | Authority on the Environment |
| --- | --- |
| `GET /v1/environments/{environmentID}/agent-revocations` | read |
| `POST /v1/environments/{environmentID}/agent-revocations` | manage-permissions |
| `DELETE /v1/environments/{environmentID}/agent-revocations/{revocationID}` | manage-permissions |
| `GET /v1/environments/{environmentID}/agent-credentials` | read |

Each change is written to the authorization audit trail with action
`AGENT_REVOCATION_ADD` or `AGENT_REVOCATION_REMOVE`, the operator, and the
record before and after.

- An addition that cannot be audited stays in force and the request fails.
  Repeat it; it is the same record and the repeat files the audit event.
- A removal that cannot be audited is put back and the request fails.

### Storage and replicas

| `REPOSITORY_KIND` | Where the deny-list is | Replicas |
| --- | --- | --- |
| `postgres` | PostgreSQL, migration `0001-agent-credentials` (component `agent-credentials`) | Any number. Each Agent request reads the list from the database; nothing is cached, so a revocation written through one replica applies to the next request on every replica. |
| `file` | This process's memory | One. **A restart empties the deny-list.** Do not rely on revocation in this mode. |

An Environment holds at most 512 revocations. The 513th is refused with `409`;
remove ones that no longer matter (an expired certificate needs no revocation).

The PostgreSQL adapter's tests need a database and **were written but not
run** for this change. They are in `scripts/test-postgres-repository.sh`.

## Seeing an expiry coming

On each admitted Agent request the Hub records the certificate presented:
serial, `NotBefore`, `NotAfter` and when it was seen. An unchanged certificate
is rewritten at most every five minutes per replica, so "last seen" can be up
to five minutes old. A changed certificate is recorded at once.

Per Agent, behind the Environment's read authority:

```sh
conduit agent-credential list prod-use2
```

returns, for each Agent, `serial`, `notBefore`, `notAfter`, `lastSeenAt`,
`secondsUntilExpiry` and, if a revocation would refuse it now, `revokedBy`.

For alerting, two gauges on the metrics endpoint (`metrics.enabled` in the Hub
chart; PostgreSQL only):

| Metric | Meaning |
| --- | --- |
| `conduit_agent_certificate_min_seconds_until_expiry` | Seconds until the soonest-expiring Agent certificate seen in the last 24 hours lapses. Negative once it has. Absent when no Agent has been seen. |
| `conduit_agent_certificates_tracked` | How many Agent identities presented a certificate in the last 24 hours. |

Neither carries a label naming an Environment or an Agent. The metrics
registry refuses such a label, because it would publish the fleet's topology
to anyone who can scrape. The metric says *something* expires soon; the CLI
command above says *which*.

An example alert. **Not exercised:** no chart here renders a rule, and this one
was not loaded into a Prometheus.

```yaml
groups:
  - name: conduit-agent-certificates
    rules:
      - alert: ConduitAgentCertificateExpiresSoon
        expr: conduit_agent_certificate_min_seconds_until_expiry < 7 * 24 * 3600
        for: 1h
        labels:
          severity: warning
        annotations:
          summary: An Agent client certificate expires within seven days
          description: Run `conduit agent-credential list <environment>` to find which.
```

An Agent whose certificate has already lapsed stops being seen, and drops out
of the gauge 24 hours later. Alert well before expiry, not at it.

## Not exercised

- The cert-manager `Issuer` and `Certificate`, on any cluster.
- A Spoke installed from the example values.
- cert-manager in a UDS deployment.
- The example alert rule.
- The PostgreSQL deny-list tests (written, not run: no database was available).

What was run: the Go tests named in
`internal/hub/composition/agent_credentials_test.go`, which drive the assembled
Hub handler with real X.509 certificates, and the two-root CA rehearsal in
`internal/controlplane/agent_ca_rotation_test.go`.
