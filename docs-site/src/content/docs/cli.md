---
title: Using the CLI
description: Configure an operator connection, inspect native resources, and submit governed changes without losing outcome evidence.
---

The `conduit` binary is the operator client. `platform-delivery` starts or
inspects a Hub, and `platform-spoke-agent` runs the Environment Agent. Their roles
and credentials differ. Build from the selected source revision as described in
[Install Conduit](/conduit/install/).

## Connect as an operator

Use a deployment-issued operator credential and the Hub's trusted CA. Keep bearer
tokens and private keys in protected files; the client enforces the expected
file permissions. For a bearer connection, for example:

```bash
conduit --server https://hub.example.invalid \
  --ca-file /secure/hub-ca.pem \
  --bearer-token-file /secure/operator-token \
  environment list
```

The example hostname is deliberately non-routable. Replace it with the
authorized deployment. The CLI also supports certificate/workload identity and
the configured client-credentials flow; inspect the generated
[connection flags](/conduit/reference/cli-commands/#connection-and-output-flags)
for the exact selected version. A human browser sign-in/refresh flow must not be
inferred from support for service credentials.

Named local profiles and `configure` save connection settings, not a grant of
server authority. The default output is JSON; `--output yaml`, `csv` and `table`
are available where the command's result supports them. Preserve machine-readable
output when recording immutable coordinates or ambiguous outcomes.

## Inspect before a mutation

```bash
conduit --profile deployment environment list
conduit --profile deployment product list
conduit --profile deployment plan list
conduit --profile deployment plan get PLAN_ID
```

Only resources visible to the authenticated principal are returned. Confirm the
Environment and its selected native Space, where the command supports that
filter. An empty or partial authorized index is not an inventory of everything
on the Hub.

## Submit and review a Plan

```bash
conduit --profile deployment plan propose --file /reviewed/desired-state.json
conduit --profile reviewer plan get PLAN_ID
conduit --profile reviewer plan approve PLAN_ID --rationale "Reviewed target and rollback evidence"
```

These are separate requests made under the intended identities. Approval remains
subject to the Hub's policy and self-approval rules. Retain the returned Plan
identity and read its recorded state; do not treat a successful proposal as an
execution receipt. A failed or interrupted write may already have taken effect,
so inspect the current record before retrying.

## Full command surface

The generated [command reference](/conduit/reference/cli-commands/) lists the
actual command tree, arguments, flags and parser defaults. It is regenerated
from the native CLI constructor without executing commands or reading operator
configuration. Help presence does not prove that an optional provider or backing
service is enabled. Explicit refusal remains meaningful; no reference-command
name implies compatibility with a private upstream service.

For programmatic callers, use the [API guide](/conduit/api/) and the exact native
contract available for that route.
