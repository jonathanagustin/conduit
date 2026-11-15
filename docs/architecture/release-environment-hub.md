# Selected release environment hub

The existing Releases Environments tab (`s14-env-detail`, also the destination of
`rm01-env-page`) selects a visible environment. The shared area header and
default inspector context follow that same selection. A selected ID absent from
the visible index never silently selects another environment.

Installation evidence is filtered by the selected environment and uses the
conservative release-convergence classification. The Hub's own selected
environment timeline and blackout decision are fetched independently using GET,
with a 15-second read timeout. Late, cancelled or detached-view responses cannot
replace the current selection. Blackout results must name the selected
environment and contain an actual boolean decision. A failed or mismatched read
is unavailable, never “none”. The view does not borrow the old hard-coded dev
timeline, fleet-wide upgrade count or global offline-bundle summary.

Recorded environment downtime/no-downtime rules show their time zone and weekly
interval strings. Recorded overrides show their provided start/end, reason and
approver. These are not resolved execution appointments. Product and installation
constraints can still apply when no environment rule is present. Recorded intervals remain read-only. The Windows view separately supports
[reviewed Entity window drafts](window-scheduler.md); it does not resolve a
workload execution schedule.

The existing explicit Propose fix action is retained for a diverged selected
installation. It uses the same `/v1/fleet/propose-fix` endpoint and records the
returned change-request ID; it grants no direct execution or approval. The click
handler rebinds the installation to the selected visible environment before
calling the existing Hub adapter. All displayed fields are escaped.

These are Conduit extensions over native Hub records. No private protocol,
computed schedule, independent audit-chain verification or deployment authority
is claimed. Remaining scheduling and dependency enhancements stay in #553.
