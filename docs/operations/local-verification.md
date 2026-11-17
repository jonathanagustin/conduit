# Local verification

Run the smallest relevant check while editing, then run the repository-owned
aggregate before handoff:

```bash
./scripts/verify-local.sh
git diff --check
```

The aggregate reads the manual Trunk workflow and runs its corresponding local
checks. A name filter is available for a bounded iteration:

```bash
./scripts/verify-local.sh Apollo
```

The complete aggregate requires Linux x86_64, matching its pinned Helm,
Hadolint, Zarf and UDS executables. It refuses another host before downloading
those files. Run the complete gate in a Linux verification checkout; a native
macOS focused check is recorded separately from that gate.

## macOS focused checks

Install Xcode Command Line Tools and confirm `xcode-select -p` succeeds before
running the installer. Apple's Git, Python and compiler stubs can exist before
their backing tools are installed. The installer checks that tools can run;
it does not invoke an installation dialog or use administrator privileges.
Record the exact toolchain, host and CGO setting with focused Go checks. A
successful command accepting `-race` alone does not establish that its detector
is active; validate the selected setup with an intentional-race control as well.
Native extensions such as Ruby JSON need the compiler setup below.
Prebuilt Go, Node and Conduit CLI binaries can execute without these tools;
the requirement applies to native compilation in the full developer setup.

The locked mise toolchain supplies Go, Node, pnpm and Ruby 3.4.12. Use the
mise-provided Ruby: macOS system Ruby 2.6 does not implement methods the
validators use. The installer then runs `mise run install-ruby-json`, which
checks the existing JSON version and, when needed, installs the SHA-256-pinned
fixed gem from the [trust review](../security/THIRD-PARTY-TRUST-REVIEW.md).
Run that task after a direct `mise install --locked` as well. Native gem
compilation can require the Command Line Tools.
The native Temporal helper selects and verifies an archive for
macOS arm64 or amd64, and keeps it separate from Linux cache entries. Demo
timestamps use the required Python interpreter and need no GNU date.

A running Podman engine can run the focused repository and workspace
isolation checks. Put its executable on `PATH`, or name its absolute path:

```bash
DEVWORKSPACE_CONTAINER_BIN=/opt/podman/bin/podman \
  go test ./internal/devworkspace/adapters/container
CONDUIT_CONTAINER_ENGINE=/opt/podman/bin/podman \
  ./scripts/test-postgres-repository.sh
```

The PostgreSQL gate also detects `docker` or `podman` on `PATH`; an explicit
`CONDUIT_CONTAINER_ENGINE` must be reachable and never falls back silently.
It uses that engine for startup, readiness, database tools and cleanup.
The workspace proof checks the actual single network membership and its
inspected `Internal` property. Podman reports its bridge driver in
`HostConfig.NetworkMode` even when the container belongs to the dedicated
internal network, so that label alone cannot establish isolation.
The capability proof reads both effective and bounding masks from the
container's kernel state. It requires both to be zero; Docker's literal `ALL`
and Podman's expanded capability names remain diagnostic launch metadata.

The full demo's image builder and registry publisher still require a
Docker-compatible executable named `docker`; selecting Podman for the two
focused checks does not configure those separate paths.

Go checks should include the changed packages and, before promotion,
`go test -race ./...` plus `go vet ./...`. UI changes need the package's unit
tests and the relevant browser contract. When a UI change affects the embedded
bundle, stage only the output that passes `scripts/test-console.sh` under Linux;
a successful Windows build is not byte-level production evidence. Apollo-facing
changes also need the pinned-spec validators.

Record unavailable prerequisites as unavailable. A Windows build failure in a
Unix-specific package is not a passing result; rerun the exact gate in Linux.
Do not convert a skipped integration into compatibility evidence.

## Go toolchain and the analysis tools

The compiler, container build images, and analysis commands pin Go 1.27.4.
The container manifests are resolved from the registry and pinned by digest.
Staticcheck v0.8.2 and govulncheck v1.9.0 support the Go 1.27 module graph.

The earlier Go 1.25.13 analysis override is retired: it scanned an obsolete
standard library and now reports reachable vulnerabilities. The November 2026
merge recovery also updates x/net, x/text, and gRPC to patched versions in the
Hub and the independently locked Cosign source build. Keep both module graphs,
the container pins, mise, and the supply-chain assertions aligned when updating.
