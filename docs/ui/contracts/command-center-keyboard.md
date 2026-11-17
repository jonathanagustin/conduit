# Command Center keyboard refresh contract

The overview's layer controls are native toggle buttons. A Hub refresh retains
focus on the selected layer control while repainting current data. It does not
freeze the overview snapshot or turn a read-only control into a write.

Activation stays on the native button path. Retained focus lets Space complete
across a refresh, including repeated keydown while held, without a custom
activation handler. Mouse and Enter behavior remain native as well.

The Command Center browser regression explicitly refreshes after focus, then
between Space down/up, including a repeated keydown. It verifies the pressed
state, focus, map-label visibility and absence of mutation requests. The old
source fails the focus assertion. This synthetic Hub test does not establish
screen-reader conformance or real fleet operation.
