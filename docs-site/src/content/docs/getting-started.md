---
title: Quick start
description: Install the pinned toolchain, start the local demo, and open the operator console.
sidebar:
  label: Quick start
  order: 1
---

Clone the repository, install the pinned toolchain, and start the local demo. Install Docker and start its daemon. Install zarf v1.0.1 and put it on `PATH`, or set `ZARF_BIN`. The installer does not install Docker or zarf and does not request administrator privileges. `scripts/demo-seed.sh` exits with the zarf archive URL and checksum when zarf is missing.

## Quick start

```bash title="terminal"
git clone https://github.com/jonathanagustin/conduit.git
cd conduit
./install.sh
mise run demo
```

`./install.sh` runs [`scripts/install.sh`](https://github.com/jonathanagustin/conduit/blob/main/scripts/install.sh). It downloads the pinned mise release asset, checks the SHA-256 checksum, and runs `mise install` against the locked toolchain. Run it again any time; work that is already done is skipped. Source `./install.sh` instead of executing it when you want mise activated in the current shell. Executing it prints the activation command.

When the seed finishes, open `http://127.0.0.1:8081/console/#s01-intake`. The seed starts the Hub with `CONDUIT_IMAGE_BUILDER=docker`. The source-packaging reproduction is [`scripts/demo-excalidraw-e2e.sh`](https://github.com/jonathanagustin/conduit/blob/main/scripts/demo-excalidraw-e2e.sh). Its printed digest is the local engine image id.

`mise run demo` runs [`scripts/demo-reset.sh`](https://github.com/jonathanagustin/conduit/blob/main/scripts/demo-reset.sh) and then [`scripts/demo-seed.sh`](https://github.com/jonathanagustin/conduit/blob/main/scripts/demo-seed.sh). You can run those two scripts directly.

## When ports cannot be published

On a Docker host that cannot publish ports because it has no iptables or bridge networking, set `DEMO_DOCKER_NETWORK=host`. The seed also switches to that mode when the default publish or image build fails for that reason.

## Recorded proposals

For live proposals, configure `CONDUIT_MODEL_BASE_URL` and `CONDUIT_MODEL_NAME` for an operator-managed HTTP endpoint. Leave model configuration unset for recorded proposals with no model call. Host model command settings are refused.

`CONDUIT_MODEL_TIMEOUT` defaults to 45 seconds. A model cannot approve a change; passing proposals still require human review. The full configuration and its tested limits are in [HTTP model endpoint](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/model-http.md).

## Where the console is built

`web/` contains the operator console. [`scripts/build-console.sh`](https://github.com/jonathanagustin/conduit/blob/main/scripts/build-console.sh) writes it into `internal/consoleui/dist` using the Node and pnpm pins in `mise.toml`. The demo scripts run that build and skip it when the output is already current. Before the build, the Hub still starts and `/console/` names the command.

## Read next

- [How it works](/conduit/how-it-works/) for the governed path the demo walks.
- [Installing and verifying a published release](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/release-and-publication.md#installing-and-verifying-a-published-release) for CLI archive, SLSA provenance, SPDX SBOM, Helm chart, and air-gap Zarf package verification and installation by digest.
- [Repository README](https://github.com/jonathanagustin/conduit/blob/main/README.md) for the same quick start beside the demo media.
- [Local verification](https://github.com/jonathanagustin/conduit/blob/main/docs/operations/local-verification.md) for what a local test result does and does not prove.
