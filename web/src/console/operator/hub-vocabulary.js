/*
 * Closed lists the console shares with the Hub, in one dependency-free file so
 * scripts/verify-console-contract.mjs can import it and compare each list to
 * the Go source in both directions. Editing a list here without the Go side
 * (or the reverse) fails that gate.
 */

/** internal/notifications/domain/notifications.go — every EventType const. */
export const EVENT_TYPES = Object.freeze([
  "BLUE_GREEN_UPGRADE_FAILED",
  "CHANGE_REQUEST_PENDING_APPROVAL",
  "INSTALLED_PRODUCT_RELEASE_VULNERABILITY_UPDATE",
  "LATEST_PRODUCT_RELEASE_VULNERABILITY_UPDATE",
  "PLAN_FAILURES",
  "PRODUCT_IN_OPEN_ENDED_RECALL",
  "PRODUCT_RECALLS",
  "PRODUCT_RELEASE_PROMOTION_FAILED",
  "RBAC_CHANGE",
  "ROLE_CHANGE",
]);

/** internal/notifications/domain/notifications.go — every Channel const. */
export const CHANNELS = Object.freeze(["EMAIL", "IN_APP", "SLACK"]);

/** internal/notifications/domain/notifications.go — every ContactKind const. */
export const CONTACT_KINDS = Object.freeze(["EMAIL", "PAGERDUTY", "SLACK"]);

/**
 * internal/governance/domain/authorization.go — OperationVocabulary, the only
 * operations a /v1/session answer lists. An operation outside it (an approval
 * token a policy demands, for one) is never in a session answer, so its
 * absence there says nothing.
 */
export const HUB_SESSION_OPERATIONS = Object.freeze([
  "artifact:create",
  "bundle:import",
  "change:propose",
  "debug-session-request:create",
  "debug-session-request:manage",
  "debug-session-request:read",
  "fleet:bulk-change",
  "label:apply",
  "module-installation:override-approve",
  "permissions:manage",
  "platform:act-on-behalf",
  "product-release:create",
  "product:create",
  "resource:operate",
  "resource:read",
]);

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
