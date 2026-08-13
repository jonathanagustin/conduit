# AWS Load Balancer Controller v3.4.3 IAM prerequisite

This versioned Terraform module creates only the IRSA role and the official,
version-matched controller policy. The policy is semantically identical to
`docs/install/iam_policy.json` at upstream tag `v3.4.3` (commit
`cbbb278ea62e8ca3e1b68808052e9e8447f1ca52`); its canonical JSON SHA-256 is
`6203c2b12b9cfad35b441b1d4d9cb70b50153fa2e402ac58cf636c7311480a56`.

The module deliberately does not attach `AmazonEC2ReadOnlyAccess`,
`ElasticLoadBalancingFullAccess`, or any other broad substitute policy. Pass the
cluster's OIDC provider ARN and issuer URL and annotate the controller Service
Account with the resulting `role_arn`. Keep those cluster-specific values in
the owning environment repository.

Before installing or upgrading the controller, run:

```console
./preflight.sh <role-arn> <policy-arn>
```

The preflight verifies that the one declared managed policy is attached, its
default document still matches this pinned source, and the role simulation
allows the unconditional actions whose absence blocks ALB realization. A
successful simulation is necessary but not sufficient: a Plan is successful
only after the Spoke observes a load-balancer address on every Ingress in the
exact deployed Helm manifest. Product health remains a separate probe.
