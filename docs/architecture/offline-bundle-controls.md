# Offline bundle control boundary

The native offline-bundle metadata endpoint requires the existing `operate` permission on `platform/root`, like bundle transfer confirmations. Authentication alone does not authorize handoff, report-back or install-review operations. The route authorization catalog and grant-negative probe exercise this decision.

Requests accept one bounded JSON object with known fields. A caller cannot assert site connectivity through `online`. Presence now reads the repository's Environment report for the site identifier, verifies that environment coordinate and applies the Hub's existing 45-second spoke-liveness rule. Missing reports, missing Agent identity, zero observation time and stale reports keep report-back pending. A fresh report indicates report freshness only; it is not proof that the offline bundle was installed.

Handoff and pending-review metadata remain process-local in the existing application service. This change does not make that state durable, change bundle formats, transfer bytes, attest an installation or implement the deferred isolation mode. Existing signature verification and separate human-review behavior remain. This hardening supports the offline-bundle UI work in #556; that issue remains open.
