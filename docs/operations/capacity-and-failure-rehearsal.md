# Capacity and failure rehearsal

Conduit does not yet have a production Environment, Agent or Plan throughput
limit. The fixtures below measure an explicitly configured control-plane
workload, rather than deriving a production bound from a development laptop.
They are Conduit-native and make no private Apollo compatibility claim.

## Repeatable load and soak

Start from a committed clean checkout with the pinned Go toolchain. The wrapper
records the commit/tree, complete output and a hash-bound success receipt outside
the checkout. It refuses a changed checkout or an existing report destination.

```bash
GOMAXPROCS=2 CONDUIT_CAPACITY_ENVIRONMENTS=10 CONDUIT_CAPACITY_AGENTS=2 \
  CONDUIT_CAPACITY_GENERATIONS=20 ./scripts/test-fleet-capacity.sh

GOMAXPROCS=2 CONDUIT_CAPACITY_ENVIRONMENTS=50 CONDUIT_CAPACITY_AGENTS=2 \
  CONDUIT_CAPACITY_DURATION=15m CONDUIT_CAPACITY_TIMEOUT=17m \
  CONDUIT_CAPACITY_GO_TIMEOUT=18m ./scripts/test-fleet-capacity.sh
```

Each Environment has one producer reviewing successive desired generations,
while M real Agent workers poll the real Hub HTTP surface. The planner,
lifecycle and repository are real. A simulated executor returns exact fixture
observations; it performs no Helm operation, image verification or Kubernetes
deployment. Successful Plans must have their own committed Environment report.
Every Environment must make progress; a failure or timeout refuses the run.

The producer starts a new generation after the previous one converges. The
repository therefore retains increasing Plan history, rather than measuring
only a steady empty queue. Poll timing and optional simulated apply delay are
recorded, as are Go/OS/architecture, CPU visibility, GOMAXPROCS, allocations and
end-of-run Go heap. Heap is not RSS or peak memory. The fixture and Hub share
one process, including when separate PostgreSQL pools serve two Hub handlers.

The report contains completed Plans/second, per-Environment completions and
full-response RPC latency through body close. Mean and maximum are exact;
p50/p95/p99 are **bucket upper bounds**, not exact quantiles. Histogram storage
is bounded for long soaks. A report is usable success evidence only with its
matching PASS receipt and log. An interrupted run can leave a partial report.

### Recorded development samples

The [retained record](../evidence/fleet-validation-2026-11-17/index.json)
contains raw reports, logs and matching receipts for source commit
`e2a4518658cf9660938ca928b56ca1cb96bdf7c7`. These are **contended workstation
functional samples**, using macOS/arm64 on an Apple M5 Pro, Go 1.27.4 and
`GOMAXPROCS=2`. Other checks and Podman workloads were active. Each memory
profile starts empty; it has one in-process Hub and simulated workload
execution. No sample establishes a supported production fleet maximum.

| Environments × Agents | Work | Completed Plans | Elapsed | Plans/s | Lease p95 upper bound | Finish p95 upper bound |
| --- | --- | --- | --- | --- | --- | --- |
| 1 × 1 | 10 generations | 10 | 0.263s | 38.00 | 5ms | 1ms |
| 10 × 2 | 5 generations each | 50 | 0.621s | 80.50 | 50ms | 50ms |
| 25 × 2 | 3 generations each | 75 | 6.312s | 11.88 | 500ms | 1,000ms |
| 5 × 2 | 30s producer, 50ms polling, 5ms simulated Apply | 273 | 30.679s | 8.90 | 500ms | 250ms |

All four profiles completed work in every Environment with zero measured HTTP
errors. The 30-second profile accumulated 273 retained Plans; it checks sustained
progress, not long-duration stability. Its 19,849 MiB of cumulative Go
allocations and 19.0 MiB end heap measure different things. The latency and
allocation growth justify repeating at deployment-specific history depths;
they cannot isolate the database, polling or concurrent-work effects here.

The separate PostgreSQL promotion sample completed 69 Plans, including 15 and
52 post-promotion completions in its two Environments. It counted 82 interrupted
lease requests and one interrupted finish request, then recovered. It is
functional recovery evidence, not an availability or throughput SLO. A
15-minute soak is reproducible with the command above but is **not** claimed by
these short recorded runs.

Configuration is supplied through `CONDUIT_CAPACITY_` variables:

| Variable | Default | Meaning |
| --- | --- | --- |
| `BACKEND` | `memory` | `memory`, `file`, or `postgres` |
| `ENVIRONMENTS` / `AGENTS` | `10` / `2` | Environment count and polling Agents per Environment |
| `GENERATIONS` | `5` | Completed generations per Environment when `DURATION=0` |
| `DURATION` | `0` | Producer duration; a nonzero value replaces generation count |
| `POLL` / `APPLY_DELAY` | `25ms` / `0` | Delay after each Agent cycle / simulated workload delay |
| `HANDLERS` | `1` | Independent in-process Hub handlers; multiple require PostgreSQL |
| `TIMEOUT` / `DRAIN` | `5m` / `30s` | Whole-run bound / required recovery allowance beyond duration |
| `POSTGRES_DSN` | unset | Dedicated disposable capacity database; never use tenant data |

The fixture refuses more than 1,000 Environments, 100 Agents per Environment,
10,000 total Agents, sub-millisecond polling or a duration above 24 hours. These
are **harness safeguards**, not supported product capacity. The PostgreSQL
profile applies normal repository migrations and keeps its generated records;
use a fresh disposable database to compare runs at the same initial depth.
The file profile uses one Hub, as the shipped runtime requires.

```bash
CONDUIT_CAPACITY_BACKEND=postgres CONDUIT_CAPACITY_HANDLERS=2 \
  CONDUIT_CAPACITY_POSTGRES_DSN="${DISPOSABLE_CAPACITY_DSN}" \
  ./scripts/test-fleet-capacity.sh
```

## Failure injection

The normal Go suite includes two real HTTP/Agent scenarios:

- `TestFleetPartitionDoesNotInventExecutionAndRecovers` closes pooled
  connections and refuses subsequent TCP dials. It requires no lease or
  observation to be invented while disconnected, then actual convergence when
  connectivity returns. It is an in-process network fault, not a cluster
  NetworkPolicy or physical packet-loss test.
- `TestFleetAgentDisconnectMidPlanIsRecoveredByASecondAgent` stops a real
  Agent during its simulated Apply call and disconnects its detached final
  report. It waits the shipped ten-second minimum lease, then requires a second
  Agent to produce real attempt-two evidence. The expired Agent's later report
  must not replace that observation.

The opt-in PostgreSQL fixture creates its own private network, two digest-pinned
PostgreSQL 18 instances and one standby data volume. It configures synchronous
`remote_apply` replication, starts traffic through two independent Hub pools,
stops the primary after a real Plan commits, promotes the standby, and switches
a TCP endpoint while traffic continues. It removes only its own containers,
network and volume, preserving its test log/report directory.
Only a matching success receipt qualifies the whole promotion scenario as
passed; a report alone can precede a later assertion failure. Every Environment
must complete at least two generations after the endpoint switches. The
producer runs beyond the real lease-expiry interval so recovery cannot exhaust
the workload window before fresh reconciliation is checked.

```bash
CONDUIT_CONTAINER_ENGINE=/opt/podman/bin/podman \
  ./scripts/test-fleet-failover.sh
```

This tests PostgreSQL process loss, streaming promotion, broken pooled
connections and application recovery. It does not test a managed database's
control plane, DNS failover, split-brain prevention, a production multi-process
Hub, or a Kubernetes workload. Transient errors are counted during the fault;
they are not silently treated as uninterrupted availability.

## Planner invariants and product bounds

`TestGeneratedFleetPlannerInvariants` generates 400 seeded DAG fleets with up
to eight Products in four namespaces. It checks complete unique installations,
dependency-before-dependent order in the same namespace, exact declared
artifacts, deterministic Plans under eight input permutations, and isolation
of changed Release content bindings. A further 200 generated cycles must be
refused. `BenchmarkGeneratedFleetPlanner` reports time/allocations at 1, 10,
100 and 170 installations.
The benchmark uses one Product in distinct namespaces. The retained
[`planner-benchmark.log`](../evidence/fleet-validation-2026-11-17/planner-benchmark.log)
checks these sizes execute; its shared-workstation timing is not an admitted
capacity limit. [Mutation controls](../evidence/fleet-validation-2026-11-17/mutation-controls.json)
confirm that removing initial ready ordering, omitting artifacts from release
bindings, and permitting cycles each fail the relevant generated test. A map
capacity-hint change passes as an equivalent control: keys, values and lookup
behavior remain identical.

The existing durable execution envelope is a real product bound: at most 512
evidence tasks per Plan. Ordinary reconcile steps reserve three tasks each plus
two fixed tasks, so 170 ordinary steps fit before secret mutations consume
additional tasks. Different actions have different costs; admission computes
the exact bound. See `internal/controlplane/diagnostics.go` and
`internal/execution/domain/execution.go`. This is a per-Plan evidence limit,
not a fleet or throughput limit.

No production capacity is admitted by the presence of these scripts. Before
stating a supported fleet limit, retain passing load and soak reports on the
intended CPU/memory/database/storage/network topology, repeat at increasing
history depth, exercise failure recovery, and choose an operator-visible SLO.
