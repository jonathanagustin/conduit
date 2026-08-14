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
