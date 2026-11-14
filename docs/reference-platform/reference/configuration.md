# Configuration contracts

Sources are indexed in `review-ledger.tsv`. These are reconstructed public
semantics, not a private format claim.

This note covers configuration an *operator* supplies for an installed Entity.
The configuration a *Product* declares about itself — `configuration.yml`, its
traits, and the substitution language — is a separate document contract in
[`product-definition.md`](product-definition.md).

## Entity config overrides

- The YAML top-level keys are starting Product versions. A Release selects the
  greatest starting version less than or equal to itself within the same major
  version. Selection never crosses a major boundary.
- Once an Entity opts into versioned overrides, absence of a matching range is
  a deployment constraint, including when the intended override is empty.
- Resolution happens with Release selection so version and configuration enter
  one Plan atomically.
- For Helm Products, the selected `overrides` object becomes top-level Helm
  values. The platform injects an authoritative `apollo` value block containing
  Entity identity, Product coordinate/version, Environment ID/config, resolved
  Kubernetes Secret names, the default artifact-store URI, and image-pull
  Secret names. User configuration cannot replace this authoritative block.

## Stop and start

- Kubernetes workloads are stopped through ordinary version-ranged
  configuration by setting the chart's desired replica value to zero. Removing
  that override restores the Product default. This preserves the normal
  approval, audit, compatibility-range, and atomic version/config behavior.
- There is deliberately no generic pause/resume mutation against live Pods:
  the chart owns the replica value's path and Kubernetes controllers own Pod
  replacement.

## Environment Config

- Environment Config is typed YAML/JSON shared across Entities. Keys may not
  contain hyphens, periods, or whitespace.
- It is available under `apollo.environment` in Helm values.
- `{{ preprocess .Values.apollo.environment.<path> }}` in Entity override YAML
  resolves the typed Environment value before YAML parsing; missing paths fail
  closed.

## Environment settings document

The public Environment settings page was rechecked on 2026-08-02 UTC against
snapshot `82b64523...f32ad`. The YAML document is an operator and policy
surface, so Platform exposes a bounded validation adapter at
`POST /v1/apollo/environment-settings/validate` and preserves fields it does
not yet model. Preserved fields still appear in semantic diff output: an
unknown setting cannot change while `diff_only_modifies_paths` sees an empty
diff. Entity locators are unique by locator name, even when two entries name
different Kubernetes namespaces.

## Local evidence

- `internal/controlplane/configuration.go` performs preprocessing, strict
  single-document decoding, same-major range selection, deep merge, and
  authoritative value injection.
- `configuration_test.go` covers most-specific selection, typed preprocessing,
  authoritative-block anti-spoofing, missing/major-bounded support, invalid
  Environment keys, unknown fields, and multiple-document rejection.
- `environment_settings_reference_test.go` covers the documented YAML vocabulary,
  HTTP validation, semantic unknown-field diffs, locator uniqueness, partial
  application, and round-trip preservation.
