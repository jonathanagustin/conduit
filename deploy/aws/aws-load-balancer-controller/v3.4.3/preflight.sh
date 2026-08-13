#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <role-arn> <policy-arn>" >&2
  exit 2
fi

role_arn=$1
policy_arn=$2
role_name=${role_arn##*/}
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)

attached=$(aws iam list-attached-role-policies --role-name "$role_name" --output json)
if [[ $(jq '.AttachedPolicies | length' <<<"$attached") -ne 1 ]] ||
   [[ $(jq --arg arn "$policy_arn" '[.AttachedPolicies[] | select(.PolicyArn == $arn)] | length' <<<"$attached") -ne 1 ]]; then
  echo "the role must have exactly the one version-matched controller policy" >&2
  exit 1
fi
inline=$(aws iam list-role-policies --role-name "$role_name" --output json)
if [[ $(jq '.PolicyNames | length' <<<"$inline") -ne 0 ]]; then
  echo "the controller role has undeclared inline policies" >&2
  exit 1
fi

version=$(aws iam get-policy --policy-arn "$policy_arn" --query 'Policy.DefaultVersionId' --output text)
deployed=$(aws iam get-policy-version --policy-arn "$policy_arn" --version-id "$version" --query 'PolicyVersion.Document' --output json)
deployed_digest=$(jq -S -c . <<<"$deployed" | sha256sum | awk '{print $1}')
declared_digest=$(jq -S -c . "$script_dir/iam-policy.json" | sha256sum | awk '{print $1}')
if [[ "$deployed_digest" != "$declared_digest" ]]; then
  echo "attached controller policy drifted from the pinned v3.4.3 document" >&2
  exit 1
fi

simulation=$(aws iam simulate-principal-policy \
  --policy-source-arn "$role_arn" \
  --action-names \
    ec2:CreateSecurityGroup \
    elasticloadbalancing:CreateListener \
    elasticloadbalancing:RegisterTargets \
    elasticloadbalancing:DescribeTargetHealth \
  --output json)
denied=$(jq -r '[.EvaluationResults[] | select(.EvalDecision != "allowed") | .EvalActionName] | join(",")' <<<"$simulation")
if [[ -n "$denied" ]]; then
  echo "controller IAM preflight denied required actions: $denied" >&2
  exit 1
fi

echo "AWS Load Balancer Controller v3.4.3 IAM preflight passed"
