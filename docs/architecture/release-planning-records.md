# Recorded release planning, ramp and window views

The existing Releases area exposes Planning (`s15-plan-detail`), Ramp
(`rm10-ramped-rollout`) and Windows (`rm09-fleet-scheduler`). Planning shares the
recorded plan execution inspector rather than the former sample dry-run,
signature, hardware-root, quorum and duration claims. Ramp shares the native
staged-wave view instead of computing pacing percentages from example cohorts.
Windows uses the selected environment hub and its recorded maintenance policy.
The existing promotion/recall governance paths are not replaced.

All three views have an explicit rollout/maintenance calendar panel. Distinct
recorded maintenance overrides with finite increasing start/end timestamps can
be drawn as read-only relative bars, ordered by start. At most 64 intervals are
shown; duplicate or invalid intervals are omitted. Labels display their actual
recorded type, installation/environment scope, timestamps, reason and approver.
A maintenance interval is not a rollout execution appointment. Plans use the
overrides on their immutable step maintenance constraints; environment/ramp views
use the selected environment's recorded overrides.

No resolved rollout calendar, dependency-timing edge or blackout interval catalog
is supplied by the native wave/blackout routes. Empty calendar evidence stays
explicit. Weekly maintenance intervals constrain execution but are not resolved
appointments. No drag, resize, snap or Save is enabled without a governed
scheduling proposal and a resolved schedule. The new views neither compute
approval nor call an execution/publication endpoint.

These are Conduit extensions over native Hub records. They make no private
protocol compatibility or independent signature/chain verification claim.
Remaining governed scheduling backend work stays in #415/#553; this UI must not
claim that the complete scheduling interaction is implemented.
