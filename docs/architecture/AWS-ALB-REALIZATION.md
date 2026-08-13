# AWS ALB prerequisite and Ingress realization

Status: **declarative and fail-closed contracts implemented; live fresh-cluster
proof remains open**.

The dev incident behind issue #210 separated two facts that Helm had previously
collapsed. A chart can be `deployed` while an asynchronous controller is unable
to realize an Ingress. In the observed failure, AWS Load Balancer Controller
v3.4.3 lacked `ec2:CreateSecurityGroup`; its Ingress had no address, but the
Plan still succeeded.

## Declarative AWS prerequisite

`deploy/aws/aws-load-balancer-controller/v3.4.3` contains a reconstructible
IRSA role and managed policy. The policy is semantically pinned to the official
upstream `v3.4.3` document and the verifier rejects drift or reintroduction of
the two broad managed-policy substitutes seen in the incident. Environment OIDC
coordinates and names remain in the owning environment repository.

The preflight verifies attachment and policy-document equality, then simulates
four unconditional, load-bearing actions: security-group creation, listener
creation, target registration, and target-health observation. This is a
necessary IAM admission check, not proof that a controller is healthy or that a
particular Ingress has reconciled.

## Plan completion contract

After exact Helm release and digest observation for a managed rollout, the
Spoke reads the manifest stored for that release. Every
`networking.k8s.io/v1` Ingress in that exact manifest must:

- exist with a Kubernetes UID and positive generation; and
- contain at least one non-empty hostname or IP in
  `status.loadBalancer.ingress`.

Missing identity, observation failure, malformed state, or an empty address
fails the Plan's observation task and triggers the existing rollback path. The
failure diagnostic is attributed to the exact Ingress and includes its
namespaced controller events, bounded and redacted by the existing execution
receipt contract. An unrelated namespace Ingress cannot satisfy the check.
The explicit no-wait rollout remains `UNKNOWN`/in-grace and does not claim this
realization evidence.

An address proves external resource realization, not application correctness.
Product dependency and representative-request health remain the separate
deep-health/promotion contract; the Spoke does not gain the controller's AWS
identity or infer target health from an address.

Executable evidence:

```text
go test -race ./internal/agent -run 'Ingress|HelmExecutor'
./scripts/verify-aws-load-balancer-controller.sh
```

Closure requires a fresh-cluster E2E using only the declared IAM policy: create
an Ingress, observe its address and healthy targets, remove
`ec2:CreateSecurityGroup`, and prove the resulting Plan fails with the
controller event in its persisted receipt.
