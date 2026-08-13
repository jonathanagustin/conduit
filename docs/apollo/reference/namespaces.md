# Kubernetes namespace contracts

Sources are indexed in `review-ledger.tsv`. Platform preserves the public
safety semantics while assigning cluster-scoped reconciliation to Flux rather
than widening the Environment Spoke's authority.

## Inventory and Entity placement

- Managed namespace names are RFC 1123 DNS labels and have stable identifiers
  plus optional descriptions.
- An Entity selects its namespace when it is created; that placement is
  immutable afterward. The same Product may be installed as separately named
  Entities in different namespaces in one Environment.
- A fixed installation must target an inventoried namespace. Namespace creation
  must be reconciled and observed by the Flux/bootstrap owner before an Entity
  Plan is approved for that namespace.

## Creation, protection, and deletion

- Apollo uses a separate namespace agent whose availability depends on the Helm
  and authentication agents. Platform maps that separation to the existing
  Flux/bootstrap controller; the ordinary Spoke remains namespace-bounded and
  never receives cluster-admin namespace privileges.
- Namespace inventory changes travel through the same reviewed desired-state
  and Change Request path as other Environment changes. A newly declared
  namespace does not authorize the Spoke to create it.
- Deletion protection defaults on. Removal requires a successfully applied
  prior generation that explicitly sets `deletionProtected: false`, followed
  by a distinct deletion generation; a merely pending unprotect proposal is
  insufficient.
- Deletion fails closed while any desired Entity still targets the namespace,
  any Entity reports installed there, or an Entity from the prior generation
  has not explicitly reported `UNINSTALLED`. Flux performs the Kubernetes
  deletion only after those admission checks and its own policy pass.

## Local evidence

- `internal/controlplane/validate.go` validates namespace identity, inventory
  membership, and same-Product multi-namespace installations without allowing
  ambiguous duplicate Product installations inside one namespace.
- `internal/controlplane/namespaces.go` enforces immutable placement,
  default-on/two-generation deletion protection, and exact reported-state
  emptiness before removal.
- `go test ./internal/controlplane -run 'TestNamespace|TestSameProduct'`
  exercises the boundary.
- `docs/architecture/GITOPS-ARGO-FLUX-FLEET.md` and `deploy/flux/` define the
  external reconciliation owner. Private namespace-agent wire compatibility is
  intentionally unclaimed.
