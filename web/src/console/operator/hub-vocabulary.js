/*
 * Closed lists the console shares with the Hub, in one dependency-free file so
 * scripts/verify-console-contract.mjs can import it and compare each list to
 * the Go source in both directions. Editing a list here without the Go side
 * (or the reverse) fails that gate.
 */

/** internal/controlplane/session.go — every SessionApplication id. */
export const HUB_APPLICATIONS = Object.freeze([
  "change-requests",
  "export",
  "fleet",
  "import",
  "plans",
  "promotions",
  "risk",
  "teams",
]);
