# Race runtime execution preflight

A zero process status is insufficient evidence that a Go race suite ran. On the
Mac's ARM64 Podman VM, pinned Go 1.27.4's Linux amd64 race runtime emitted
`SanitizerTool: CHECK failed: sanitizer_allocator_primary32.h:292` before
`testing` started, then returned zero. An unconditional failing test and a
real data race also returned zero without executing their bodies (#900).

`scripts/verify-race-runtime.sh` compiles one small race-instrumented test binary
with the caller's actual Go toolchain and target. It runs three controls:

- A protected concurrent counter must reach its body marker and explicit PASS
  with status zero.
- An unconditional failure must reach its own marker and explicit FAIL with a
  nonzero status.
- Unprotected concurrent writes must reach their marker, emit an actual
  `WARNING: DATA RACE`, and return nonzero. A halt-on-error configuration may
  terminate before the test framework prints FAIL.

Every control must enter its named test. Known sanitizer startup failures are
rejected even if status zero or unrelated success text is present. Missing
markers, incorrect statuses and missing detector output fail verification. The
fixture has no application dependencies and each test has a 30-second timeout.
The workflow's Go lane runs the preflight before its suite; the complete local
verifier runs the same named gate and reconciles both script invocations with
the workflow. A failed preflight invalidates the complete verification result.

`scripts/test-race-runtime-preflight.sh` supplies controlled process transcripts
to exercise refusal cases and verifies that all three controls remain invoked.
It also checks that the workflow and local gate retain the required ordering.
The real preflight must still pass on the actual execution platform; transcripts
are regression checks for the guard, not evidence that a race runtime works.

## Local runner evidence and limits

The original runner image
`defcd743797716e567a5cf683b0c99b6d92114c55d7191af38ee2aaebad3c1c5`
and an image changing only the Go host compiler both reproduced the startup
failure with the same Linux amd64 test ELF. Their Go 1.27.4 amd64 race runtime
bytes matched SHA-256
`5bdd6b292a1d7bb4a21c3134bafec7545f23133e2e640fed265a7f8c5b67a638`.
The VM was aarch64 with qemu-x86_64 10.2.2. This demonstrates failure of that
execution configuration; it does not establish an application test result or a
failure on native Linux amd64 hardware.

Native Darwin arm64 Go 1.27.4 with `CGO_ENABLED=0` passed the positive control,
rejected the unconditional failure and reported the deliberate race. A bounded
per-process `QEMU_GUEST_BASE=0x800000000000` trial also made all three Linux
amd64 controls behave correctly on this VM. Reserving 128 TiB with
`QEMU_RESERVED_VA` instead failed to allocate the vsyscall page, and setting
`GORACE=exitcode=66` alone did not repair startup.

The guest-base setting is a candidate local runner configuration, not a source
workaround or full-verification receipt. It changes no VM/binfmt/global setting
and is never installed by the guard. A complete exact-head run must record its
real image, target and environment and pass the preflight and every existing
gate. Older runs with unverified race execution remain incomplete evidence.
