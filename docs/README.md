# Conduit documentation

This directory documents Conduit as it exists and the contracts it is moving
toward. It is not a copy of the former Platform documentation set.

Living documentation is organized by responsibility:

- `architecture/` records Conduit's current boundaries, intended dependency
  direction, and explicitly named gaps.
- `decisions/` records accepted architectural choices and their confirmation
  gates.
- `reference-platform/` describes the bounded Apollo integration context and its pinned
  public compatibility profile.
- `operations/` contains the orchestration doctrine, assurance and harness
  requirements, tenant-fidelity, verification, and release guidance.
- `security/` states the trusted-technology and authority boundaries, including
  the [threat model](security/THREAT-MODEL.md), [selected control
  evidence](security/CONTROL-EVIDENCE.md), and [external review commissioning
  scope](security/EXTERNAL-REVIEW-SCOPE.md).
- `ui/` contains the console's interface contracts and its navigation as
  coded, beside dated evidence of earlier consoles. `ui/README.md` says which
  is which.

Where to start for a given question:

- What is proven and what is not:
  [`operations/PRODUCTION-READINESS.md`](operations/PRODUCTION-READINESS.md).
- Configuring a model provider:
  [`operations/model-http.md`](operations/model-http.md) for an
  OpenAI-compatible HTTP endpoint,
  [`operations/local-model-cli.md`](operations/local-model-cli.md) for a local
  command.
- What the model-assisted paths do and refuse:
  [`operations/app-generation.md`](operations/app-generation.md),
  [`operations/appgen-deploy.md`](operations/appgen-deploy.md),
  [`operations/planning-source-checkouts.md`](operations/planning-source-checkouts.md).
- Protecting and auditing the Hub:
  [`operations/hub-rate-limiting.md`](operations/hub-rate-limiting.md),
  [`operations/authorization-audit-integrity.md`](operations/authorization-audit-integrity.md),
  [`operations/route-authorization.md`](operations/route-authorization.md).
- Notifications:
  [`operations/native-notification-delivery.md`](operations/native-notification-delivery.md).
- Zarf and UDS: [`operations/uds-package.md`](operations/uds-package.md) and
  [`../deploy/zarf/README.md`](../deploy/zarf/README.md).
- Releasing: [`operations/release-and-publication.md`](operations/release-and-publication.md)
  and [`operations/manual-ci.md`](operations/manual-ci.md).
- The console: [`ui/console-navigation.md`](ui/console-navigation.md) and
  [`../web/README.md`](../web/README.md).

`operations/` has no index of its own; the list above is a starting point, not
a complete one.

Evidence is different from documentation. Pinned upstream contracts, fixtures,
receipts, sanitized observations, security reports, and generated admission
records are retained under the relevant `evidence/` trees or beside the
validator that consumes them. Their presence proves only the claim named by the
record. It does not make an old design narrative authoritative and does not
establish private Apollo behavior.

When a living document and an immutable evidence record disagree, record the
conflict as unknown or a bounded gap. Do not rewrite evidence to match an
implementation claim.
