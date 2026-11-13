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

The compiler, container build images, and analysis commands pin Go 1.26.11.
The container manifests are resolved from the registry and pinned by digest.
Staticcheck v0.8.2 and govulncheck v1.9.0 support the Go 1.26 module graph.

The earlier Go 1.25.13 analysis override is retired: it scanned an obsolete
standard library and now reports reachable vulnerabilities. The November 2026
merge recovery also updates x/net, x/text, and gRPC to patched versions in the
Hub and the independently locked Cosign source build. Keep both module graphs,
the container pins, mise, and the supply-chain assertions aligned when updating.
