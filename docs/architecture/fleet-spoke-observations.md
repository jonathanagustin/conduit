# Spoke and workload observations

Spoke Operations (`s17-spokes-audit`) replaces the example relay, diode and courier board with the visible native environment catalog and fleet convergence records. The environment button selects that exact visible environment in the existing environment hub. The selected environment hub also shows only that environment's recorded workload report.

Liveness, Agent identity and observation time come from the Hub's `spoke` projection. A stale report remains stale; a missing state remains unknown. Workloads are Agent-reported entity installations with namespace, version, release digest, health, lifecycle and observation time. These are historical fields, not current Kubernetes measurements. There is no cluster-wide inventory, utilisation or topology claim. Displayed environment and workload lists are bounded at 256 with an explicit truncation notice. Values are escaped.

This native view is registered as `reporting.SpokeWorkloadView` and marked `CONDUIT_NATIVE`. The public spoke control-plane responsibility boundary in `docs/reference-platform/reference/spoke-control-plane.md` does not establish a private cluster telemetry API. No additional Agent or cluster permission is introduced.

This is part of #556. The offline bundle workflow and report-back controls remain separate work. This view never synthesizes pending-sync status, transfer progress, cryptographic verification or installation receipts from report freshness. Existing environment timeline, blackout, convergence and governed fix actions are retained.
