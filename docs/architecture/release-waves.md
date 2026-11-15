# Recorded rollout waves

The existing Releases area's Waves tab reads `/v1/rollouts` through the existing
Hub refresh. Each rollout shows its recorded ID, product/version, status and
pause actor. Waves preserve backend order and display recorded name, environment
IDs, gate, status and reason. Text is escaped. No operation, confirmation or
approval is added by this view.

This is a Conduit extension over the native staged-rollout record. The record
has no calendar start/end timestamps, maintenance-window definitions or resolved
dependency edges. The view explicitly declares scheduling unavailable; wave order
is not elapsed time, percentage completion or an inferred dependency edge.
An unavailable index and a rollout with no wave details remain explicit. A failed
blackout read is no longer rendered as “none” alongside these records. The old
refresh still reads the legacy blackout summary for other existing views.

This is part of #553. Scheduling, dependency graph and timeline enhancements
remain tracked there and are not claimed by the wave inspection view.
