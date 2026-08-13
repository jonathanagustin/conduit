# OR-Tools CP-SAT admission

Status: **OR-Tools 9.15 availability is verified. `discrete.schedule.v1`, its
exact lexicographic Go compiler/verifier/process contract, the official-C-API
Rust/native adapter, and a pinned source-build container recipe are implemented
but UNADMITTED. The local amd64 OCI candidate has passed its complete native,
isolation, source/link/license-closure, Syft/SPDX, and current Grype gates;
zero CP-SAT images or artifacts are admitted or published**. The exact local
result and its remaining gates are retained in
[the amd64 candidate evidence](../evidence/or-tools-9.15-cpsat-candidate-amd64.json).

The repository now also contains a manual-only native multi-architecture
distribution workflow and a fail-closed cross-architecture evidence compiler.
They require an exact clean `main` revision, independently verified amd64
and arm64 image archives, a cache-evicted exact-image rebuild on each
architecture, byte-identical semantic output, a protected
environment decision, digest-addressed push, Sigstore signatures, SPDX/Grype
attestations, and SLSA provenance. They deliberately emit only `UNADMITTED`
distribution evidence and have not been run; therefore this status and the zero
published or admitted count do not change.

This note fixes the Phase 2 implementation boundary from the
[optimizer toolchain plan](OPTIMIZER-TOOLCHAIN-PLAN.md). It is an artifact and
protocol decision, not permission for a solver to approve, lease, schedule, or
execute Platform work.

## Upstream baseline

As of 2026-08-12, [OR-Tools v9.15 is the latest stable
release](https://github.com/google/or-tools/releases/tag/v9.15). The release
anchors source commit `551ad10d94835c99e5e1e684500d3db398c0e345` and
provides hash-labelled AlmaLinux 8.10 C++ archives for both x86-64 and arm64 and
an Apache-2.0 source archive. Local replay verified the source, two C++ archives,
and two runtime NuGet packages: five of 122 published assets totaling
184,151,099 bytes. It also verified both upstream ELF architectures and the five
exported CP-SAT C functions; only amd64 received an upstream native execution
probe in that release-corpus audit. The later Platform-owned amd64 candidate
execution is separate evidence retained below. Exact upstream values are
retained in [the release evidence](../evidence/or-tools-9.15-release.json).

The bounded C API is useful: it accepts serialized `CpModelProto` and
`SatParameters`, supports an interruptible environment, and returns a serialized
`CpSolverResponse`. The response distinguishes `UNKNOWN`, `MODEL_INVALID`,
`FEASIBLE`, `INFEASIBLE`, and `OPTIMAL`. Official troubleshooting documentation
also says `UNKNOWN` includes reaching a search limit before another state is
known; Platform must not translate it to infeasible.

## Why the published runner is not the Platform backend

The official `sat_runner` is a model-file and command-line tool rather than the
strict bounded JSON stdin/stdout protocol already admitted by the generic
Platform process host. More importantly, each published `libortools` has 105
direct dynamic requirements and the archive retains 123 regular `lib64` files.
The closure includes CBC/CLP/COIN-OR, HiGHS, and SCIP/SoPlex even though the
Platform candidate needs only CP-SAT. The release README itself warns consumers
to review SCIP licensing.

Copying that archive into a runtime would therefore:

- expand the dependency, CVE, license, and symbol surface far beyond CP-SAT;
- duplicate the separately controlled HiGHS backend;
- require a large mutable dynamic-library search path;
- leave malformed-protobuf abort behavior and response-buffer ownership inside
  an unversioned adapter boundary; and
- still fail to bind Platform request, model, objective, and receipt identity.

The binary archives are availability evidence and native comparison inputs only.
They are not distribution candidates.

The seven bundled license/notice-named files are not a complete manifest for
the 105-entry dynamic closure. Abseil, protobuf, re2, HiGHS, COIN-OR,
zlib/bzip2, libc, libstdc++, and compiler-runtime ownership and licenses require
independent source/link manifests. The two NuGet runtime packages contain no
bundled license files and only declare Apache-2.0 metadata.

## Rust boundary

The protocol-facing adapter source is Rust because it is a small
memory-sensitive translation boundary and this keeps unsafe code localized.
Rust does not replace the Go domain/compiler/verifier or make OR-Tools a Rust
solver. The Go host owns canonical protobuf construction and parsing; Rust
treats those protobufs as bounded opaque bytes.

Do not make a third-party Rust binding the trusted boundary. The recently
published `cp_sat` 0.4.1 still requires an externally installed OR-Tools and
protobuf closure and currently fails its docs.rs build. `cpsat-rs` 0.1.2 is a
very new wrapper whose own documented limitations include no cancellation and
plain integer variable handles without model provenance; its docs build also
fails. These projects are useful design evidence, not admitted dependencies.

The repository now contains a strict Rust JSON envelope and a Platform-owned
C++ bridge linked to the official five-function `SolveCp*` C API under
`//ortools/sat/c_api:cp_solver_c`. The bridge parses and reserializes both
protobufs before the upstream abort-on-malformed boundary, catches C++
exceptions, bounds signed lengths and response bytes, and pairs the upstream
allocation with its exact free path. Rust owns one environment per request and
coordinates deadline interruption with solve/destruction so a stop cannot race
environment deletion.

The eventual Platform adapter must own this narrow contract:

1. Go-authored protobuf bytes conform to the three hash-pinned 9.15 source
   contracts and are parsed independently by the Go host;
2. one audited FFI module exposes an exact, measured, source-built ABI with
   interrupt, allocation, and destruction ownership;
3. cancellation always uses the interruptible environment before an outer
   process deadline is allowed to kill the sandbox;
4. lengths are bounded below the C API's signed-`int` limit, malformed input is
   rejected before FFI, and the exact upstream allocator/free pairing is gated;
5. request JSON and response JSON remain strict, bounded, and free of paths,
   URLs, credentials, environment-derived behavior, and arbitrary arguments;
6. the adapter authors only discrete choices and bounded raw proof/status; Go
   reconstructs and independently verifies all domain-owned results; and
7. the final image contains one Rust adapter plus a source-built minimal CP-SAT
   closure, not the official monolithic `lib64` tree.

`Cargo.lock`, the Rust toolchain image, crate archives, `protoc`, the OR-Tools
source archive, and the C/C++ toolchain are immutable build inputs. Standard
SBOM output must be complemented by a linker-owned static/dynamic closure and
license manifest, following the HiGHS lesson.

## Model contract comes before the wrapper

The current allocation model accepts binary64 demand and capacity. CP-SAT is an
integer solver. Silently multiplying those values by a convenient constant
would change feasibility and objective ordering while leaving the model revision
unchanged. Phase 2 therefore uses a new discrete scheduling model with explicit
integer units and no implicit float conversion.

The first revision should bound and identify:

- indivisible work, integer duration, release/deadline windows, and exact
  eligible execution lanes;
- precedence DAGs and same/different topology or failure-domain relationships;
- integer resource demands/capacities over explicit dimensions;
- maintenance exclusions and ownership-defined time origin/granularity;
- rollout concurrency and a bounded-disruption/concurrency primitive; full
  Kubernetes PDB semantics still require replica health/readiness and eviction
  evidence;
- ordered primary and secondary integer objectives with exact units; and
- snapshot, constraint, model, scaling, and objective digests.

The current unadmitted Go implementation owns this `discrete.schedule.v1` request,
receipt, independent result verifier, deterministic raw-protobuf compiler, and
strict external-process envelope. Request validation rejects cycles, absent
units, impossible windows, incomplete costs/topology, and unbounded collection
sizes. Compiler validation rejects domains, rows, topology expansion, cumulative activity, and
objectives outside CP-SAT's conservative half-int64 range; sets seed zero, one
worker, and both absolute and relative gap limits to zero; and binds the exact
integer `inner_objective_lower_bound`. Wall-time is bounded; deterministic-time
controls remain admission work. It compiles ordered exact makespan, optional
cost, and stable per-task lane/start-fixing phases. Each phase preserves its
exact incumbent, inner bound and gap in a
`CP_SAT_EXACT_LEXICOGRAPHIC_V1` trace; only a complete trace may claim the
canonical optimum. No CP-SAT backend is selectable or admitted.

Returned starts, resources, and ordering choices are independently checked in
Go. A CP-SAT `OPTIMAL` claim additionally requires a complete objective/unit
proof whose bound equals its incumbent. A time-limited incumbent remains
`TIME_LIMIT` plus a verified result; caller cancellation/deadline remains
`UNKNOWN`, and `UNKNOWN`, model-invalid, process error, and infeasible remain
distinct. The process watchdog reserves response/shutdown grace before the
outer deadline, but native timeout-with-incumbent behavior is not yet executed.

An amd64 20-run upstream probe produced one semantic assignment but 20 raw
response digests because upstream serializes volatile wall/user timing. Receipt
identity therefore hashes the parsed semantic proof, not unknown/volatile
protobuf fields or solver-internal auxiliary choices. The compiler-owned fixing
phases now close tied optima in the model contract. The Platform-owned OCI then
produced one byte-identical canonical semantic receipt over five race-tested
native runs. Admission still requires its native arm64 counterpart and exact
cross-architecture comparison.

## Artifact plan

The candidate builds from the exact 9.15 source archive rather than the binary
bundle. `platform_targets.BUILD.bazel` retains the audited C API target and adds
only the narrow bridge, Rust static archive and adapter executable. The pinned
recipe uses Rust 1.97.0, Bazelisk 1.29.0, Bazel 8.7.0 and digest-pinned builder
and distroless runtime images; it refuses emulated cross-building, strips the
binary, statically links compiler runtimes, records build inputs, runs as UID
65532 and labels the result `UNADMITTED`. On amd64, the 9,442,301-byte image
contains the pinned distroless-static rootfs plus one candidate layer and a
37,713,640-byte stripped static executable. Five native semantic runs passed
under a networkless, read-only, capability-free, non-root, CPU/PID/memory-bounded
sandbox. Embedded link/source/license/static-closure evidence, Syft and SPDX
documents, and Grype against valid database schema `v6.1.9` found zero matches.
That content-addressed development receipt is not admission, publication, or an
arm64 result.

Admission requires:

- the corresponding native arm64 source, toolchain, crate, binary/image, SBOM,
  scan, license, and linker-closure receipt, followed by exact cross-architecture
  semantic comparison;
- strict parser/model mutations plus architecture, dependency, topology, PDB,
  maintenance-window, timeout-with-incumbent, malformed-protobuf, cancellation,
  allocator, and output-bound failures;
- repeated deterministic semantic receipts with seed zero and one worker,
  compiler-owned canonical fixing among tied optima, and exact
  cross-architecture comparison;
- cache-independent reproducibility evidence or an explicit bound on all
  nondeterministic build metadata;
- same-input native-reference shadow calibration through
  `SolverShadowCalibration`; and
- signed/attested immutable distribution and an explicit release decision.

Until every gate passes, backend selection contains no CP-SAT candidate and the
native reference remains the disconnected fallback.
