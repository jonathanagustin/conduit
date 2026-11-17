# Conduit

[![Documentation](docs-site/public/badges/docs.svg)](https://jonathanagustin.github.io/conduit/)

Conduit is a proprietary delivery control plane for governed software rollout
across connected and disconnected environments. A central Hub owns desired
state, approval, artifact admission, and durable history. Outbound-only Spoke
agents execute leased Plans inside tenant environments and report what they
observe.

The repository was extracted from the former `platform/` subtree. It now owns
its architecture, contracts, documentation, and release workflow. The exact
source boundary is recorded in [MIGRATION.md](MIGRATION.md).

## Demo

![Conduit Excalidraw Packaging & App Generation Demo](docs/ui/conduit-excalidraw-demo.webp)

The still above is a recorded console walkthrough. The command that
reproduces the source-packaging run is
[`scripts/demo-excalidraw-e2e.sh`](scripts/demo-excalidraw-e2e.sh). It needs a
running Docker daemon and `zarf` v0.82.0 on `PATH` (or `ZARF_BIN`). If either
is missing, the script exits with the install location and checksum instead of
reporting a package.

```bash
./scripts/demo-excalidraw-e2e.sh
```

That script checks out `https://github.com/excalidraw/excalidraw` at
`a2ec2889babf7d2295469c6d90ebe77fae57df84`, runs planning turns with the
explicitly configured HTTP model, records Accept, then
runs post-accept compute. Compute commits a workspace `Dockerfile`,
`zarf.yaml`, and UDS `Package` CR, builds the image, and runs the dockerfile
gate, the Package CR schema gate, `zarf dev lint`, and `zarf package create`.
It opens a change request and does not approve it. The printed digest is the
local engine image id. It is not a registry manifest digest unless that image
is pushed. The console profile for this path is `onboard`. Work intake is
`/console/#s01-intake`.

Problem-statement generation uses profile `appgen`. After `mise run demo`,
`scripts/demo-generate.sh` runs that path. Its package gates are `zarf dev lint`
and `zarf package create`. It also leaves a change request for a person.
The generator accepts exactly one of `main.go`, `main.py`, or `index.html`.
The chart deploys that application, and the package resource exposes its
Service. `mise run appgen-deploy` requests the running Service.

<details>
<summary>Planning-only walkthrough ending at Accept (<code>COMPUTE_BEFORE_ACCEPT</code> enforced)</summary>

![Conduit Excalidraw Planning-Only Walkthrough](docs/ui/conduit-excalidraw-planning-only.webp)

</details>

## Quick start

Clone the repository, install the pinned toolchain, and start the local demo:

```bash
git clone https://github.com/jonathanagustin/conduit.git
cd conduit
./install.sh
mise run demo
```

`./install.sh` runs [`scripts/install.sh`](scripts/install.sh). It downloads
the pinned mise release asset, checks the SHA-256 checksum, and runs
`mise install` against the locked toolchain. Run it again any time; work that
is already done is skipped. The installer does not install Docker and does
not request administrator privileges. Source `./install.sh` instead of
executing it when you want mise activated in the current shell. Executing it
prints the activation command.

Install Docker and start its daemon before the demo. Install `zarf` v0.82.0
and put it on `PATH`, or set `ZARF_BIN`. `scripts/demo-seed.sh` and
`scripts/demo-excalidraw-e2e.sh` exit with the archive URL and checksum when
either prerequisite is missing. The seed starts the Hub with
`CONDUIT_IMAGE_BUILDER=docker`, which admits the local image builder, the
engine image inspector, and the zarf package creator together. When the seed
finishes, open <http://127.0.0.1:8081/console/#s01-intake>.

`mise run demo` runs [`scripts/demo-reset.sh`](scripts/demo-reset.sh) and then
[`scripts/demo-seed.sh`](scripts/demo-seed.sh). You can run those two scripts
directly. On a Docker host that cannot publish ports because it has no
iptables or bridge networking, set `DEMO_DOCKER_NETWORK=host`. The seed also
switches to that mode when the default publish or image build fails for that
reason.

Optional: live model proposals. Configure `CONDUIT_MODEL_BASE_URL` and
`CONDUIT_MODEL_NAME` for an operator-managed HTTP endpoint before starting
the Hub. Leave model configuration unset to keep recorded proposals and make
no model call. Host model commands are refused. See
[HTTP model endpoint](docs/operations/model-http.md).

To install and verify a published release (CLI archives, SLSA provenance, SPDX
SBOMs, digest-pinned Helm charts, container images, and air-gap Zarf packages)
without building from source, see
[Installing and verifying a published release](docs/operations/release-and-publication.md#installing-and-verifying-a-published-release).

## Current status

Conduit has working native contexts for catalog, environments, governance,
delivery, execution, reporting, artifacts, identity, and optimization. A large
compatibility facade remains under `internal/controlplane`; the DDD refactor is
not complete until behavior and consumers move behind narrow context ports.

Apollo is a bounded integration context. Conduit targets compatibility with a
pinned public Apollo API profile and keeps public contracts, sanitized tenant
observations, Conduit extensions, and unknown behavior distinct. It does not
claim Palantir's private Hub-to-Agent protocol or an unqualified Apollo
superset. See [Apollo conformance](docs/reference-platform/conformance.md).

The contract-first API migration is also incomplete. The current Apollo path
generates route metadata, while request and response DTOs and much of the
server surface remain handwritten. The target is:

```text
pinned OpenAPI or Conjure
        -> generated DTOs, strict interfaces, router, and client
        -> bounded adapter
        -> Conduit application ports
        -> native domain contexts
```

Generated transport types stop at the adapter. They do not become delivery,
governance, execution, or persistence models.

## Repository map

- `cmd/` contains the Hub, Spoke, CLI, operator, scanner, publisher, and
  optimizer entry points. Some binary names retain the `platform-` prefix
  during migration.
- `internal/` contains native contexts, adapters, composition, and the
  temporary `controlplane` facade.
- `api/` contains pinned generated-contract inputs that are owned by this
  repository.
- `web/` contains the operator console. `./scripts/build-console.sh`
  writes it into `internal/consoleui/dist` using the Node and pnpm pins in
  `mise.toml`. `scripts/demo-reset.sh` and `scripts/demo-seed.sh` run that
  build and skip it when the output is already current. Before the build, the
  Hub still starts and `/console/` names the command.
- `deploy/` contains reusable Helm, Flux, AWS, local, and Zarf assets. Tenant
  inventories, credentials, namespaces, and installations belong to their
  deployment repositories.
- `docs/` contains the curated living documentation and retained evidence.

Published guides are at <https://jonathanagustin.github.io/conduit/>.
Start with the [documentation index](docs/README.md),
[bounded-context map](docs/architecture/bounded-contexts.md), and
[native reconciliation design](docs/architecture/reconciliation.md). Production
delivery follows the [Gitless GitOps design](docs/architecture/gitless-gitops.md):
Git for review, manually published signed private OCI artifacts for deployment,
and no Git credentials in production clusters.

## Local verification

Use focused tests while editing, then run the repository aggregate in Linux:

```bash
task verify
git diff --check
```

The aggregate is `./scripts/verify-local.sh`. Windows can run many focused
packages, but process-group code in the optimization backends is Unix-specific;
a Windows failure there is not a Linux result. Run the exact full gate in a
Linux workspace before promotion.

See [local verification](docs/operations/local-verification.md) for the limits
of each result.

## Change workflow

`main` is protected and is the only long-lived branch. Work lands through a
short-lived branch and a squash-merged pull request. The pull request records
the source and unsupported scope of every material Apollo-facing claim.

Hosted Actions minutes are deliberately controlled, so publication, release,
signing, and deployment workflows start only when a person dispatches them.
Verification is automatic: the **Trunk gate** runs on every pull request against
`main` and verifies that pull request's exact head commit, cancelling the run a
new push supersedes. Run local verification first anyway — a runner is a slow
place to discover a formatting error. A successful gate authorizes neither
merge, release publication, nor tenant deployment.

The complete flow is in [CONTRIBUTING.md](CONTRIBUTING.md),
[manual CI](docs/operations/manual-ci.md), and
[release and publication](docs/operations/release-and-publication.md).

## Tenant fidelity and secrets

Do not invent Apollo behavior or tenant configuration. A missing fact remains
unknown until a pinned public contract or an authorized, sanitized observation
establishes it. Runtime tenant overrides must be versioned, typed data resolved
by the server; they cannot carry executable code or weaken security policy.

Credentials stay in the approved environment or secret provider. Publication
may load an authorized `.env` file by reference, but its values must never be
printed, copied into artifacts, committed, or returned in logs.

See [tenant fidelity](docs/operations/TENANT-FIDELITY.md) and the
[trusted technology baseline](docs/security/TRUSTED-TECHNOLOGY-BASELINE.md).

