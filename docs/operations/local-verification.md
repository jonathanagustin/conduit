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

## Unavailable: Lint and vulnerability scanning under Go 1.26

`Lint` (staticcheck) and `Scan for known vulnerabilities` (govulncheck) cannot
run against this tree, locally or in CI, and this is not an environment defect.

`go.mod` declares `go 1.26.0` with `toolchain go1.26.6`, both introduced on
trunk by `bbbb0cf chore: extract Conduit onto protected trunk`. Neither tool
has a release able to parse the Go 1.26 standard library:

```
staticcheck 2025.1.1  -> module requires at least go1.26.0, but Staticcheck was built with go1.24.7
staticcheck @latest   -> file requires newer Go version go1.26 (application built with go1.25)
govulncheck v1.6.0    -> switches to go1.25.13, then: package requires newer Go version go1.26
```

`@latest` was tried explicitly; bumping the pin does not help. Because
`platform-ci.yml` resolves its toolchain with `go-version-file: go.mod`, CI
installs the same 1.26 toolchain and fails identically — the gates have been
red on trunk since the version bump, unobserved because the workflow is
`workflow_dispatch` and nobody has dispatched it.

The tree does not depend on 1.26 language features: setting `go 1.25.0` builds
clean (`go build ./...`, exit 0). So the options, in preference order, are:

1. wait for a staticcheck/govulncheck release that supports Go 1.26;
2. lower the `go` directive (and `toolchain`) to 1.25 until they catch up,
   which restores both gates at no known cost;
3. keep 1.26 and accept both gates as unavailable.

Do not treat their absence as a pass. Until one of the above is chosen,
`verify-local.sh` reporting `failed: Scan for known vulnerabilities; Lint` on
an otherwise-clean tree is the expected result, and says nothing about the
change under review.
