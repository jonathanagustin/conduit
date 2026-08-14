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
changes also need the pinned-spec validators. Run Zenith conformance at the
commit boundary:

```bash
node tools/zenith-protocol/scripts/conform.mjs
```

Record unavailable prerequisites as unavailable. A Windows build failure in a
Unix-specific package is not a passing result; rerun the exact gate in Linux.
Do not convert a skipped integration into compatibility evidence.

## Go toolchain and the analysis tools

`Lint` (staticcheck) and `Scan for known vulnerabilities` (govulncheck) both run
and both pass. Keeping them that way constrains the `go` directive, so change it
deliberately.

`bbbb0cf chore: extract Conduit onto protected trunk` set `go 1.26.0` /
`toolchain go1.26.6`. No release of either tool can parse the Go 1.26 standard
library, `@latest` included, so both gates failed on every tree built with that
toolchain — locally and in CI alike, since `platform-ci.yml` resolves its
toolchain with `go-version-file: go.mod`. They had been red on trunk since the
bump, unobserved because the workflow is `workflow_dispatch` and nobody had
dispatched it.

The tree never needed 1.26: it builds clean at `go 1.25.0`, so the `go`
directive is 1.25.0 and staticcheck is pinned to `v0.7.0`, which is built with
Go 1.25 (the previous `2025.1.1` is built with 1.24.7 and fails against a 1.25
module). govulncheck `v1.6.0` needs no change.

`toolchain` deliberately stays `go1.26.6`. Lowering it also drags the
digest-pinned container builds off 1.26.6 — `go env GOVERSION` inside
`golang:1.26.6-bookworm` reports the toolchain go.mod names, which would fail
`Containerfile.optimizer-highs`'s `GOVERSION` assertion and quietly defeat the
point of pinning that image by digest. Instead the two analysis gates set
`GOTOOLCHAIN=go1.25.13` for themselves only, in both `verify-local.sh` and
`platform-ci.yml`.

Before raising the `go` directive again, check that both tools have a release
built with that Go version. Raising it first silently disables the lint and
vulnerability gates, and a dispatch-only workflow will not tell you.
