# Control-plane design patterns

This note records a bounded comparison against shallow snapshots of mature CNCF
control-plane projects. It is architectural input, not a compatibility claim.

## Reviewed snapshots

| Project | Commit | Relevant pattern |
| --- | --- | --- |
| [Argo CD component architecture](https://github.com/argoproj/argo-cd/blob/62e447f2637d0af6e1b7718843e212852aa055f2/docs/developer-guide/architecture/components.md) | `62e447f2637d0af6e1b7718843e212852aa055f2` | Layered deployable components and explicit replaceable client interfaces |
| [Flux 2 CLI composition](https://github.com/fluxcd/flux2/blob/d708f5e2b791d4b3fcbe497e02806cb0fdae84b3/cmd/flux/main.go) | `d708f5e2b791d4b3fcbe497e02806cb0fdae84b3` | Cobra composition for a user CLI and validation at the command boundary |
| [Crossplane composition reconciler](https://github.com/crossplane/crossplane/blob/0d3645b65d83f1d7f89e598c8d3acbbd4d1c0a2a/internal/controller/apiextensions/composition/reconciler.go) | `0d3645b65d83f1d7f89e598c8d3acbbd4d1c0a2a` | Small reconcilers, option-based dependency injection, bounded reconcile contexts, immutable revisions |
| [cert-manager controller startup](https://github.com/cert-manager/cert-manager/blob/ff4a023e4c78f14f017c12b554d821829a1f7eb8/cmd/controller/app/start.go) | `ff4a023e4c78f14f017c12b554d821829a1f7eb8` | Versioned typed process configuration with defaulting, conversion, validation, and explicit flag precedence |

The clones were fetched with `--depth=1` into a disposable directory outside
this repository on 2026-08-02.

## Platform decisions

1. **Dependencies point inward.** Domain and application services define narrow
   outbound ports. HTTP, filesystem, memory, Helm, OCI, and external-process
   integrations implement adapters. A capability must not depend on the full
   repository merely because the executable can provide it.
2. **Composition stays at executable boundaries.** `cmd/` wires concrete
   adapters, identities, timeouts, and lifecycle. Functional options are useful
   for optional infrastructure dependencies; they are not a substitute for
   required typed inputs.
3. **Reconciliation is retry-safe.** Persisted revisions and execution evidence
   are immutable or optimistic, retries recognize an already-equal result, and
   every external operation has a bounded context.
4. **Configuration is typed and fail-closed.** Process flags, environment
   variables, and an optional versioned configuration document may be adapters
   over one validated configuration type. Product manifests, desired state,
   Plans, and governance records never use permissive map merging or coercion.
5. **Ownership is explicit.** Platform retains its outbound-only Spoke and
   reviewed Plan model. It does not adopt Kubernetes CRDs, controller-runtime,
   finalizers, or cluster-wide watches merely because the compared projects use
   them; those would change the established trust and ownership boundary.

## Cobra and Viper

Cobra is appropriate when a Platform binary grows a real subcommand surface.
For long-running controller configuration, the stronger pattern in the reviewed
cert-manager snapshot is Cobra/pflag plus a versioned typed configuration
object, explicit defaulting/conversion, deterministic flag-over-file precedence,
and validation before startup. Viper is therefore not a default dependency. It
would be considered only for process configuration after strict unknown-key,
type, precedence, secret-redaction, and conformance tests exist; it is excluded
from domain documents and persisted control-plane state.

## Applied example

The label-definition capability uses a narrow
`LabelDefinitionApplicationRepository` outbound port and keeps governed,
idempotent execution in the application service. HTTP only translates identity
and requests, while memory and filesystem repositories enforce atomic reference
and revision constraints at the last safe persistence boundary.
