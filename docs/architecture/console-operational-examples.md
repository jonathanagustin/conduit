# Operational example screens

Operational fixtures are gated at renderer dispatch, before the fixture renderer
is called. The inventory is explicit in `operational-examples.js`:

| Prototype renderer | Current home |
| --- | --- |
| Saved views | Legacy saved-view screen |
| Scope and plan | Legacy scope/plan screen |
| Backlog | Legacy backlog screen |
| Active work | Legacy active-work screen |
| Fleet overview | Command Center → Example fleet |
| Legacy environment overview | Legacy renderer; normal links redirect to the native environment view |
| Environment activity | Releases → Activity |

The default state contains no operational sample rows. “Show example data” opts
into only the current view. A notice labels its rows and controls as a local
prototype rather than Hub records or execution results, with “Exit example mode”
always available. Navigation, a native operator identity change, or reload clears
the choice. A detached old view cannot opt into another view. This is a display
choice, never an authorization grant or a failed-read fallback.

Native Plan inspectors, approval boards, workspaces, ontology, dependency graphs,
maintenance scheduling, directories and other Hub-backed views bypass this gate.
The debug terminal already shows an unavailable session-brokering state; its old
fixture markup is unreachable and is not an example-mode entrypoint.
Personal-token and proposal-trace examples have their own explicit controls
(documented alongside these). The component gallery and data-source request
shapes are documentation, rather than operational evidence.

An unrecognized screen now shows a bounded unavailable state instead of falling
back to the example fleet. No native write, conversation store or API transport
is added by this change. The remaining typed route-family migrations stay open
in #393.
