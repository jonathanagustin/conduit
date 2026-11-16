# Proposal trace examples

The proposal trace prefers native packaging records. With no recorded proposal,
it shows an empty state and an explicit “Show proposal example data” choice.
A failed or empty Hub read does not select the example mode.

Proposal trace and evaluation examples share a local, ephemeral opt-in. A visible
notice states that their controls simulate results: they do not run a model,
approve a change or apply a Plan. Exiting resets the simulated proposals, trace
banner, evaluation score and trace inputs. Reload starts opted out. When a native
proposal appears, the trace closes and resets the simulation; losing that native
row does not revive examples.

Recorded gate results remain readable without opting into simulated evaluations.
The mixed evaluation view labels its sections instead of receiving a blanket
Example data header. Empty trace views have an Unavailable header.

Simulated trace, accept/reject and evaluation actions are guarded while closed.
They do not emit authorization or review audit evidence. Native model-proposal
controls remain on the existing recorded-run path and require an explicit submit.

Unit checks cover missing, empty, malformed and refused native evidence, explicit
opt-in/reset, direct and keyboard actions, and native takeover. Chromium checks
exercise real delegated handlers, native refresh, and no native mutation request.
