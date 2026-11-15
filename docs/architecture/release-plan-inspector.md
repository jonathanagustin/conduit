# Recorded plan inspector and environment timeline

The existing `rm03-plan-detail` inspector is reachable through the Releases
area's Plans tab. It selects from the visible `/v1/plans` index and displays the
recorded ID, status, environment, timestamps, failure and planned steps. Those
steps are instructions, not per-step execution receipts. No fabricated raw
output, duration, signature verification or corrective change-request link is
shown. An explicit selected ID missing from the index does not select another
plan.

The inspector reads `GET /v1/environments/{environmentID}/timeline` for the
selected plan's recorded environment. This is an environment-wide timeline,
including other plans. It preserves backend order. Audit rows are labeled
“Hub-verified chain link” only when the response reports verified=true and
supplies a positive integer sequence and nonempty digest. Plan rows are never
labeled chain-verified. The browser does not independently verify the chain.
At most 512 returned rows are displayed, with truncation explicitly noted.

Selection changes cancel the preceding read. Response application is bound to
the current request and mounted view so a late response cannot replace another
plan's timeline. Missing or unavailable data stays explicit. All displayed text
is escaped. The view performs only GET reads and creates no execution or approval
path. These are Conduit extensions over native Hub records, not private protocol
compatibility claims. Other scheduling and dependency work remains in #553.
