# Optimization

Optimization is an advisory context. It reads bounded evidence, evaluates
alternatives, and returns content-addressed recommendations. It cannot approve,
lease, or execute a Plan.

## Evidence model

An optimization request binds a snapshot, objective, constraint evidence, and
problem definition. Source, revision, observation time, and digest make the
input replayable. Unknown, stale, conflicting, or unavailable facts remain
explicit; missing capacity is not encoded as zero cost or unlimited supply.

Recommendations are independently replayed and rehashed before downstream
outcome evidence relies on them. Created time is recorded but excluded from the
content identity so the same inputs and solver result have the same identity.

## Solver boundary

The solver protocol binds:

- model and constraint digests;
- objective digest;
- preferred backend and ordered, explicitly authorized fallbacks;
- executable digest, version, and architecture;
- deterministic settings and resource limits; and
- termination state and independently checked result evidence.

An unavailable or failed preferred backend does not authorize an arbitrary
fallback. Only the ordered names in the request may be tried. Unknown,
infeasible, feasible, optimal, time-limited, and failed outcomes remain
distinct.

## Candidate admission

A candidate backend such as CP-SAT or HiGHS begins in shadow mode. Enabling it
requires a finite-lived, content-addressed admission record for the exact
artifact coordinate. Backend admission covers artifact identity,
multi-architecture support, independent constraint verification, bounded or
reproducible build, sandbox isolation, signed distribution, and shadow
calibration.

Admission records do not grant delivery authority. The lifecycle record must
also allow selection at the time of use, and the selected executable must still
match the admitted digest and architecture. Expired, incomplete, or mismatched
evidence fails closed.

The current code implements solver request and receipt verification, lifecycle
records, calibration records, and file-backed evidence stores. Operational
admission of a published candidate remains a release decision and must not be
inferred from the presence of candidate code or a successful local test.

## Disk-pressure preflight

The capacity model accepts scheduler capacity, per-node DiskPressure state,
free bytes and inodes, exact OCI transfer and unpacked demand, placement,
reserves, and disruption constraints. Missing node observations, unknown
DiskPressure, or insufficient storage produce a blocking assessment.

This is currently advisory infrastructure. Provider collectors and Hub binding
are not complete. Before this can guard delivery, the approved proposal must
name the preflight record and the Hub must revalidate its freshness and binding
at approval and immediately before leaseability. See
[Deployment invariants](deployment-invariants.md).

## Outcome calibration

Recommendations should be compared with realized schedule, cost, reliability,
and value evidence after execution. Evaluator and solver trials retain raw
unknowns and disagreements rather than converting them to success. Calibration
may change future admission or selection policy; it may not retroactively alter
the evidence for an executed Plan.
