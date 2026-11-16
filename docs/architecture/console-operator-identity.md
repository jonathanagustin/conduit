# Console operator identity

The header and Settings use the existing native Hub session identity. The badge
shows the effective username only after the session has a complete native actor
and effective identity. A missing, malformed or refused session displays
“Identity unavailable” with an accessible description and a question-mark
avatar. Settings does not claim a signed-in principal in that state.

The live controller clears the display at an identity boundary before the next
operator's data finishes loading. It does not reuse a name supplied in initial
console state. Initial markup, ordinary shell renders and live refresh share
the same presentation rules. Real users named “Operator” remain valid.

Names are written through textContent; Settings escapes its HTML. Avatar
initials are presentation, not permission or delegation authority. At phone
width the accessible badge retains its full name while the visible text is
collapsed. The header wraps when needed so both identity and live retry remain
reachable, and the stream-status label is bounded without losing its full
title or accessible description.

This is a bounded #393 presentation correction. It adds no session endpoint,
credential source, authority check, stream reconnect or write request. Existing
native identity partitioning and authorization remain in force. Header KPIs
already derive from authorized Hub rows; other example-data and route-family
migrations remain separate.
