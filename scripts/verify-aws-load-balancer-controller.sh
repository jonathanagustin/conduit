#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
module="$repo_root/deploy/aws/aws-load-balancer-controller/v3.4.3"
expected=6203c2b12b9cfad35b441b1d4d9cb70b50153fa2e402ac58cf636c7311480a56

actual=$(jq -S -c . "$module/iam-policy.json" | sha256sum | awk '{print $1}')
if [[ "$actual" != "$expected" ]]; then
  echo "AWS Load Balancer Controller v3.4.3 policy digest drifted: $actual" >&2
  exit 1
fi

if rg -n 'AmazonEC2ReadOnlyAccess|ElasticLoadBalancingFullAccess' "$module" --glob '!README.md' --glob '!**/testdata/**'; then
  echo "broad substitute policy was added to the declarative module" >&2
  exit 1
fi

terraform fmt -check -recursive "$module"
bash -n "$module/preflight.sh"
PATH="$module/testdata:$PATH" "$module/preflight.sh" \
  arn:aws:iam::123456789012:role/controller \
  arn:aws:iam::123456789012:policy/controller >/dev/null
if FAKE_AWS_MODE=broad PATH="$module/testdata:$PATH" "$module/preflight.sh" \
  arn:aws:iam::123456789012:role/controller \
  arn:aws:iam::123456789012:policy/controller >/dev/null 2>&1; then
  echo "preflight accepted a broad substitute policy" >&2
  exit 1
fi
if FAKE_AWS_MODE=denied PATH="$module/testdata:$PATH" "$module/preflight.sh" \
  arn:aws:iam::123456789012:role/controller \
  arn:aws:iam::123456789012:policy/controller >/dev/null 2>&1; then
  echo "preflight accepted a denied load-bearing action" >&2
  exit 1
fi

echo "AWS Load Balancer Controller v3.4.3 prerequisite verified"
