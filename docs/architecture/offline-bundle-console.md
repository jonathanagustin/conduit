# Offline bundle console

The existing Bundle Preview view selects a visible stored Bundle instead of silently choosing the latest one. Its four steps are selection, verification of the existing signature, downloading the signed envelope for physical copy, then explicit operator handoff and report-back. It does not build or sign a new bundle. The selected manifest preview is bound to the same envelope and catalog digest; unverified or mismatched coordinates disable download and handoff.

The download is the existing signed JSON envelope returned by the Hub. It is not a new archive format, a transfer receipt or proof of a physical copy. The operator explicitly confirms the digest and site before recording handoff. Sites come from visible native environments. The Hub requires platform operate permission for writes.

Handoff receipts must match the bundle, digest, site and native session actor. Report-back binds to that accepted handoff and permits only the displayed operator-reported outcomes. The UI never supplies connectivity, approves a request, imports a bundle or executes a workload. A pending check-in stays pending. A returned review coordinate can be inspected through the existing Change Requests view. Neither report metadata nor a fresh Agent report attests installation of the bundle.

Editing a draft clears confirmation. Current session identity, selection, signature and visible environment are checked before each write. At most one handoff and report write are attempted per confirmed scope during the running console instance, including after navigation or an uncertain response. No automatic retry occurs. Reloading the application does not preserve that attempt cache. Handoff/pending-review server records remain process-local; durable handoff history is not claimed.

The catalog shows at most 256 options with an explicit notice. Values are escaped. Unknown identity, failed verification and unavailable APIs leave controls disabled. The native workflow is registered as `delivery.OfflineBundleConsole` with `CONDUIT_NATIVE` disclosure. No private Apollo workflow, network-isolation mode or new signing/publication authority is claimed.

As an integration fix, background Hub/settings refreshes preserve an already mounted Work Intake form. Explicit planning updates still rerender it; a refresh cannot erase a focused repository/ref draft before its input event reaches the handler. Browser coverage checks focus and value survive an awaited native refresh.

This contributes to #556. Existing workload observations, environment timeline, blackout and convergence views are retained. Cluster-wide telemetry and a durable site pending-sync projection are not fabricated by this flow.
