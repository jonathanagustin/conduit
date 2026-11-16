# Personal-token examples

The Hub does not issue personal API tokens. Preferences therefore shows that
limitation and no token rows or simulated create/revoke controls by default.

“Show token example data” explicitly opens a local simulation. Its persistent
Example data notice explains that the controls do not issue or revoke credentials.
The controls say “Create example token” and “Revoke example”. The action handlers
also refuse simulated changes while the mode is closed.

“Exit token example mode” discards all simulated rows and the draft, including
name, scope and expiry. Reentry starts from the fixed sample; reloading the console
starts opted out. The mode is not stored in browser storage and does not call a
token endpoint. Appearance preferences remain available without opening examples.

This is the personal-token slice of #393. Other prototype screens still need
explicit example-mode treatment; this change does not establish a global mode or
migrate their data clients.

Unit checks cover default refusal, explicit entry, escaping, local-only actions,
exit/reset and direct handler calls while closed. The Chromium scenario exercises
the real delegated click handlers, create/revoke, exit/reentry and reload, and
records any unexpected native mutation requests.
