# The reference material preparer

Issue #848. A Product that needs a registry credential or an application
secret depends on an executable the Agent runs: `EXTERNAL_MATERIAL_PREPARER`.
Until now every Environment had to write its own. `platform-material-preparer`
is a reference implementation that needs nothing but Kubernetes.

It copies material that is **already in the cluster**. An Environment keeps
source Secrets in one namespace; the preparer copies the keys a Plan asks for
into a Secret in the namespace the Plan targets. It does not talk to a vault.
Something else puts the source Secret there (see "Composing with External
Secrets Operator").

What has not been run on a cluster is marked **not exercised** and listed at
the end.

## The contract the Agent defines

This is `internal/agent/material.go`. The preparer implements it; it does not
extend it.

- The Agent starts the executable with **no arguments**.
- It writes **one JSON request** to standard input.
- It **discards** standard output and standard error.
- It reads the **exit status**. Zero is success. Anything else fails the step,
  and the Plan's failure reads `external material preparer failed: exit status N`.

So the exit status is the entire response. There is no response body.

The request envelope:

```json
{
  "apiVersion": "platform.delivery/v1alpha1",
  "kind": "RegistryAccess | ImagePrePull | SecretMutation | SecretMutationRollback",
  "namespace": "<the namespace the Plan step targets>",
  "access": { }
}
```

`access` for `SecretMutation` and `SecretMutationRollback`:

```json
{
  "action": "CREATE | REPLACE | VERIFY | DELETE",
  "namespace": "example-product",
  "current":  { "assignment": "database", "revision": 8,
                "providerRef": "kubernetes-secret://database-source",
                "kubernetesSecretName": "database-credentials", "keys": ["password"] },
  "previous": { "assignment": "database", "revision": 7,
                "providerRef": "kubernetes-secret://database-source",
                "kubernetesSecretName": "database-credentials", "keys": ["password"] }
}
```

`access` for `RegistryAccess`:

```json
{ "assignmentId": "primary", "revision": 4,
  "credentialRef": "kubernetes-secret://registry-source",
  "imagePullSecretName": "registry-pull" }
```

These come from the Environment's `ExternalSecretAssignment` and registry
assignment (`internal/environments/domain/configuration.go`). `providerRef`
and `credentialRef` are opaque to the Hub; this preparer gives them a meaning.

## What this preparer does with a request

### The reference grammar

`kubernetes-secret://NAME` — the Secret called `NAME` in the **source
namespace**. There is no namespace part, so a Plan cannot point the preparer
at a Secret anywhere else. Any other reference is refused (exit 4).

### Secret mutations

| Action | Apply (`SecretMutation`) | Roll back (`SecretMutationRollback`) |
| --- | --- | --- |
| `CREATE` | Write `current.kubernetesSecretName` holding exactly `current.keys` from the source | Delete it |
| `REPLACE` | The same; if the Secret name changed, delete the previous one | Write `previous` again; if the name changed, delete `current` |
| `VERIFY` | Change nothing. Fail (exit 7) unless the Secret exists, is this assignment's, is at `current.revision` and has a value for every key | Nothing |
| `DELETE` | Delete `previous.kubernetesSecretName`. Already absent is success | Write `previous` again |

The target Secret is type `Opaque`, holds only the requested keys, and carries:

- label `app.kubernetes.io/managed-by: platform-material-preparer`
- annotations `platform.delivery/material-assignment`, `-revision`, `-source`
  and `-kind`

### Registry access

Writes `imagePullSecretName` as a `kubernetes.io/dockerconfigjson` Secret, from
a source Secret that must itself be of that type and hold valid JSON under
`.dockerconfigjson`. Both `credentialRef` and `imagePullSecretName` are
required; an assignment with only one is refused (exit 4).

### Image pre-pull

**Declined**, exit 3. Answering success would have the Agent record that
images were pre-pulled when nothing pulled them. A Plan step that requires
pre-pull fails with this preparer. An Environment that needs pre-pull supplies
a different executable.

### Rules that hold for every request

- **Managed namespaces only.** A request for a namespace not listed in
  `MATERIAL_PREPARER_NAMESPACES` is refused (exit 4) before any API call.
- **Never touch a Secret it did not create.** If the target exists without the
  managed-by label, or belongs to another assignment, it is left alone and the
  request is refused (exit 4). This applies to deletes and rollbacks too.
- **Idempotent.** Running the same request twice gives the same Secret. A
  write that loses a race is retried once.
- **No secret bytes anywhere but the API.** Nothing is written to standard
  output. Standard error names objects and keys, never values. Arguments are
  refused. An API error is reported as its HTTP status only, because an API
  error body can quote the rejected Secret.

### Rollback restores from the source as it is now

The preparer keeps no history. Rolling back a `REPLACE` re-reads the Secret
`previous.providerRef` names and writes that. If revision 7 and revision 8
both point at `kubernetes-secret://database-source`, a rollback writes whatever
that Secret holds now, which is the new value. To make rollback restore the old
bytes, give each revision its own source Secret
(`database-source-v7`, `database-source-v8`).

## Exit statuses

| Status | Meaning | What to do |
| --- | --- | --- |
| 0 | Done | |
| 1 | Misconfigured: not in a Pod, no ServiceAccount credentials, or `MATERIAL_PREPARER_NAMESPACES` empty or malformed | Fix the deployment |
| 2 | Not a request this version reads: bad JSON, wrong `apiVersion`, unknown kind or action, no keys, arguments passed | Version mismatch between Agent and preparer |
| 3 | Unsupported: image pre-pull | Use a preparer that can pre-pull |
| 4 | Refused on a rule: unmanaged namespace, reference outside the grammar, a target Secret the preparer does not own | Fix the assignment, or the namespace list |
| 5 | Source unusable: absent, wrong type, or missing a requested key | Fix the source Secret |
| 6 | The Kubernetes API refused or was unreachable, including RBAC denials | The only status worth retrying unchanged. Check the Roles below |
| 7 | `VERIFY` found the Secret absent or not what the Plan says | The Secret drifted or was removed |

The Agent discards standard error, so the status is all that reaches the Plan.
To see the reason, run the preparer by hand in the Agent's Pod with the same
request on standard input.

## Configuration

| Variable | Set by | Meaning |
| --- | --- | --- |
| `EXTERNAL_MATERIAL_PREPARER` | Spoke chart `materialPreparer.command` | Read by the Agent (`cmd/platform-spoke-agent/main.go`). A clean absolute path. |
| `MATERIAL_PREPARER_NAMESPACES` | Spoke chart, from `rbac.managedNamespaces`, when a command is set | Comma-separated namespaces the preparer may write into. Required. |
| `MATERIAL_PREPARER_SOURCE_NAMESPACE` | Not rendered by the chart | The source namespace. Defaults to the Pod's own namespace. |
| `MATERIAL_PREPARER_SERVICEACCOUNT_DIR` | Not rendered by the chart | Where the ServiceAccount token, CA and namespace are read. Defaults to the standard projected path. |

The preparer inherits the Agent's environment, and reads
`KUBERNETES_SERVICE_HOST` and `KUBERNETES_SERVICE_PORT` from it.

Because the chart has no value for `MATERIAL_PREPARER_SOURCE_NAMESPACE`, a
chart-installed Agent's source namespace is the Agent's release namespace.

Turning it on (`examples/material-preparer/spoke-values.yaml`):

```yaml
materialPreparer:
  command: /usr/local/bin/platform-material-preparer
rbac:
  managedNamespaces:
    - example-product
```

`Containerfile.agent` builds the binary to that path. **The image was not
built for this change** (no container runtime was available). The Containerfile
lint and `scripts/verify-trusted-supply-chain.rb` pass, and the `go build`
command the Containerfile runs was run by hand and produces a static binary.
Until an image is built and inspected, treat "the Agent image ships the
preparer" as a claim about the Containerfile, not about a published image.

## RBAC

The preparer has no identity of its own. It is a process in the Agent's Pod
and uses the Agent's ServiceAccount token. It makes four kinds of request and
no others; it never lists or watches.

| Namespace | Resource | Verbs | Scoped by name |
| --- | --- | --- | --- |
| Source namespace | `secrets` | `get` | Yes: `resourceNames` listing each source Secret |
| Each managed namespace | `secrets` | `get`, `create`, `update`, `delete` | No: `create` cannot be scoped by name |

The source-namespace rule, exactly (`examples/material-preparer/source-namespace-rbac.yaml`
has it with its RoleBinding):

```yaml
rules:
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get"]
    resourceNames: ["database-source", "registry-source"]
```

Do not add `list` or `watch`. Kubernetes cannot scope them by name, so either
would let the Agent read every Secret in the source namespace. With the
default source namespace that includes the Agent's own client key.

In each managed namespace the Spoke chart's Role (`rbac.create`) already
grants the Agent `get`, `list`, `watch`, `create`, `update`, `patch` and
`delete` on Secrets, for Helm's release records. The preparer needs four of
those and the chart adds nothing for it. With `rbac.create=false`, bind the
four verbs yourself.

**The preparer adds no isolation.** The Agent already holds every permission
the preparer uses. The namespace check and the ownership check stop a *Plan*
from steering the preparer somewhere it should not go; they do not constrain
the Agent.

## Composing with External Secrets Operator

**Not exercised.** This repository installs no External Secrets Operator, no
`SecretStore` and no vault, and the example below has not been applied to a
cluster.

The composition:

1. A vault holds the material.
2. ESO syncs it into a Secret in the source namespace and keeps it current
   (`examples/material-preparer/external-secret.yaml`).
3. An `ExternalSecretAssignment` names that Secret:
   `providerRef: kubernetes-secret://database-source`.
4. When a Plan runs, the preparer copies the requested keys into the target
   namespace.

The preparer never contacts the vault and holds no vault credential.

Two things follow from the copy happening when a Plan runs, not continuously:

- ESO refreshing the source Secret does **not** update the target. The target
  changes when a Plan carries a `SecretMutation` — in practice, when the
  assignment's `revision` is raised.
- The same applies to rollback, as described above.

Any controller or person that produces a Secret in the source namespace
composes the same way. ESO is the example, not a requirement.

## Tested, and not

Tested (`go test ./internal/materialpreparer/... ./cmd/platform-material-preparer/...`):

- The Agent's own caller (`agent.NewExternalMaterialPreparer`) starts the built
  binary and drives registry access, create, verify, rollback, an unmanaged
  namespace and image pre-pull, against a fake Kubernetes API that enforces
  the RBAC rules above and refuses everything else.
- Secret values are absent from standard output, standard error, request
  paths and the Agent's error, including when the API quotes the object in an
  error body.

Not exercised:

- The preparer in a real Pod against a real API server.
- A chart that requires a secret, installed end to end through a Plan. The
  issue asks for this; it needs a cluster and is still open.
- The Agent image containing the binary.
- External Secrets Operator, or any vault.
