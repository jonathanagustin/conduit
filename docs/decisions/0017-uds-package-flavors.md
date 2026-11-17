# ADR 0017: UDS package flavors

Status: **Proposed** (2026-11-17), issue #832. This records the state of the
package and what each option would require. It decides nothing: adopting or
declining a flavor is the repository owner's call.

## Context

UDS packages are conventionally published in flavors that differ in where their
images come from: `upstream` (the project's own images), `registry1` (Iron Bank
images from Registry One) and `unicorn` (Defense Unicorns' hardened images).
Zarf implements a flavor as a create-time filter: a component carrying
`only.flavor` is included only by `zarf package create --flavor <name>`, and
`zarf package publish` appends the flavor to the package tag (VERIFIED against
the pinned Zarf v0.82.0 `--help`; the three flavor names and what each sources
are INFERRED from UDS convention, not read from this repository).

The Conduit package has no flavors. `deploy/zarf/zarf.yaml` has no
`only.flavor`, the release workflow passes no `--flavor`, and the two images it
carries are Conduit's own:

- the Hub, built from `Containerfile` as `FROM scratch` over runtime files
  taken from a digest-pinned `cgr.dev/chainguard/static` stage;
- the Spoke Agent, built from `Containerfile.agent` on that same digest-pinned
  `cgr.dev/chainguard/static` image.

Both are built, scanned, signed and published by this repository. In UDS terms
that is one flavor, `upstream`, based on Chainguard static. It is published
without a flavor suffix as
`ghcr.io/<owner>/<repo>/packages/platform-runtime:<version>`. No `registry1`
or `unicorn` variant exists anywhere under `deploy/`.

## Options

1. **Stay single-flavor (the current state).** Nothing to build. A consumer
   that requires Iron Bank or unicorn images cannot use the package as shipped.

2. **Name the existing flavor `upstream`.** Add `only.flavor: upstream` and
   publish as `<version>-upstream`. This changes the published tag, so the
   lifecycle lane, the README commands and any bundle referencing the package
   change with it. It adds no capability by itself.

3. **Add `registry1` and/or `unicorn`.** Conduit's images are its own, so this
   is not a matter of swapping an upstream image reference for a hardened one.
   Each added flavor would require:
   - a hardened build of the Hub and of the Spoke Agent on that flavor's base,
     with its own digest, SBOM, vulnerability evidence and signature, through
     the same publication workflow;
   - for `registry1`, Iron Bank acceptance of those images, which is an
     external approval process this repository does not control, and registry
     credentials in the publication workflow;
   - one more package build per architecture per flavor, and the
     chart-equivalence and validator gates run per flavor;
   - a decision on which flavor the unsuffixed tag means, or dropping it.

## Not decided here

Which option to take, and whether any consumer requires a non-upstream flavor.
No consumer requirement is recorded in this repository (UNKNOWN). Until one is,
option 1 is simply what exists.
