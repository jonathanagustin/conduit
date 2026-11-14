# Apollo CLI public workflow contract

## Versioned coverage

The implemented command-tree profile remains the 105-command capture dated
2026-08-15 in `../cli-command-contract.json`, with source hashes pinned under
`../snapshots/cli-command-sources-2026-08-15.tsv`. The independently refreshed
`../cli-command-inventory.json` records 123 public command pages as of 2026-11-13.
Its 18 additions are discovered evidence, not implemented command claims.
Both inventories have independent hash and coverage gates; see ADR 0008.

## Reviewed workflow semantics

Reviewed 2026-08-02 UTC from the public getting-started page, command reference,
and 45 Apollo CLI release-note entries recovered through v0.814.0 in the APOLLO
documentation corpus. This note records observable semantics; it does not
preserve copyrighted page bodies or establish native binary equivalence.

## Distribution and invocation

- The compiled `apollo-cli` binary is downloaded from the Hub Settings UI for
  the selected operating system. Public documentation does not describe a
  Maven-distributed Hub, Agent, or CLI.
- Commands use nested verbs, support `--help`/`-h`, and publish shell completion
  instructions through `apollo-cli completion <shell> --help`.
- The documented top-level read/publish families are `environment`, `entity`,
  `product`, and `product-release`.

## Profiles and authentication

`apollo-cli configure` writes named profiles to
`~/.palantir/apollo-cli.config`; `default` is used unless `--profile` selects
another. Connection values may also be supplied through flags or environment
variables, but this page does not define their exact precedence or config-file
encoding.

The four documented token-provider modes are `auto`, `static`, `service-user`,
and `sso`:

- `static` uses one Multipass bearer token;
- `service-user` uses a client ID and client secret;
- `sso` performs interactive browser login; and
- `auto` selects static when a bearer token exists, service-user when both
  service credentials exist, and SSO otherwise.

Static and service-user credentials are mutually exclusive. Reconfiguration
must clear obsolete fields. CI guidance exposes a bearer token through an
`APOLLO_TOKEN` secret; values are never product metadata.

Configuration also carries optional default Environment and Space IDs plus
global gates for v1/v2 experimental and deprecated commands, debug tools, and
Mission Manager experimental/deprecated features.

## Observable command shapes

`environment list` defaults to a human table and accepts `-o json`. The public
JSON example is an array whose objects contain:

- `apolloEnvironmentID`;
- `environmentName`;
- `contactTeamName`;
- `declaredStateSource`;
- `reportedStateSource`; and
- `spaceID`.

Helm publication is a two-stage workflow. `product-release helm-chart init`
can consume either an OCI repository coordinate (`--name`, `--version`, and
`--repository-url`) or a local archive (`--chart-path` plus the repository
URL). It emits either an Apollo Product TGZ with `--as-tgz`, or manifest/default
configuration/schema inputs for `product-release create`. Even when init reads
a local archive, deployment later resolves the chart by the repository URL,
chart name, and chart version; uploading metadata does not upload the runtime
chart to that repository.

The release-note chronology makes the archive handoff concrete:

1. `product-release helm-chart init --as-tgz` creates the Product Release
   archive;
2. the archive contains `deployment/manifest.yml`,
   `deployment/values.yaml`, and, when the chart supplies one,
   `deployment/values.schema.json`;
3. v0.814.0 `product-release create` reads those configuration files back from
   the archive rather than requiring separate arguments; and
4. Product publication remains distinct from the runtime OCI chart upload.

The documented Module path is likewise one connected workflow:

```text
apollo-cli module lock --module-directory <directory>
apollo-cli module evaluate --feature product-dependencies -f <definition-or-lock>
apollo-cli module create --module-directory <directory>
apollo-cli module install module:<name>:<version> -e <environment-id>
```

`module lock` flattens composite Modules and supports `--verify` for CI.
`module evaluate --feature product-dependencies` checks outbound direct Product
dependencies across the Module and its submodules; v0.796.0 added `.lock` files
as inputs. v0.795.0 added batch creation from `.yml`, `.yaml`, and `.json`
definitions in `--module-directory`, including co-located `version: local`
references. Installation accepts either a Product ID or an exact Maven
coordinate; the exact coordinate pins the installed Module release.

`product-release asset publish` packages a file or directory using
`product-type: asset.v1`, addressed by a Maven coordinate. Optional `--images`
coordinates declare container artifacts that Apollo checks for availability.

## Platform consequence and evidence limit

- The existing public API, reconstructed Helm Product adapter, Module lock and
  publication service, and Flux-reconciled publisher cover the evidenced
  server-side workflow, not this complete CLI
  command tree, profile store, table renderer, completion output, or Asset
  archive bytes.
- A future platform-native CLI should use the standard Go command ecosystem,
  keep credentials out of arguments and repository state, support deterministic
  JSON for automation, and treat repository publication as separate from
  Product metadata publication.
- Command names and archive member semantics above are public ground truth.
  Exact help text, configuration encoding, tar ordering, gzip metadata, file
  modes, and native output bytes still require an authorized Hub-downloaded
  binary capture. Public documentation cannot close that byte-equivalence gap.
- CI credentials remain pipeline-owned secrets and exactly one authentication
  mode is supplied. CI packages and pushes immutable chart/image artifacts
  before publishing their Maven-coordinate metadata. A Hub-downloaded native
  CLI is external evidence, not a binary this repository vendors or guesses.

## Primary source

- <https://www.palantir.com/docs/apollo/apollo-getting-started/apollo-cli-getting-started/>
- <https://www.palantir.com/docs/apollo/core/whats-new/>
- <https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_product-release_helm-chart_init/>
- <https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_product-release_create/>
- <https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_module_lock/>
- <https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_module_evaluate/>
- <https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_module_create/>
- <https://www.palantir.com/docs/apollo/apollo-cli/apollo-cli_module_install/>
- Snapshot SHA-256:
  `930ec8831d2da8368edbcd999fab3854f82ee00a1799dd174f8504b6902750da`
