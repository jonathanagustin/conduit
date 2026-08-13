import { Tag, type Intent } from "@blueprintjs/core";

/** One place where a Hub status string becomes a colour. Screens must not
 * decide this individually: the same status has to read the same everywhere,
 * and an unrecognised status must stay visible rather than silently render as
 * "normal". */
const HEALTH_INTENT: Record<string, Intent> = {
  HEALTHY: "success",
  UNHEALTHY: "danger",
  INDETERMINATE: "warning",
  UNKNOWN: "none",
  ERROR: "danger",
  TERMINAL: "danger",
  WARNING: "warning",
  REPAIRING: "warning",
};

const ROLLOUT_INTENT: Record<string, Intent> = {
  UP_TO_DATE: "success",
  OUT_OF_DATE: "warning",
  IN_PROGRESS: "primary",
  FAILED: "danger",
};

const CONFIG_INTENT: Record<string, Intent> = {
  APPLIED: "success",
  DRIFTED: "warning",
  PENDING: "primary",
  UNKNOWN: "none",
};

const LIFECYCLE_INTENT: Record<string, Intent> = {
  INSTALLED: "success",
  UNINSTALLED: "none",
  RELINQUISHED: "warning",
  PENDING: "primary",
};

/** States that need no attention. A table where every cell shouts is a table
 * where nothing does: reserving solid intent for what is wrong is what makes
 * UNHEALTHY findable in a screen of two hundred rows. The plan asks for
 * "modest radii" and for status to be paired with text as well as colour, which
 * these tags already are -- the word is the primary signal, the fill is the
 * secondary one. */
const CALM = new Set([
  "HEALTHY",
  "INSTALLED",
  "UP_TO_DATE",
  "APPLIED",
  "SUCCEEDED",
  "APPROVED",
  "PASSED",
]);

function statusTag(table: Record<string, Intent>, value: string) {
  // An unmapped value is shown as-is with a neutral, outlined tag rather than
  // dropped, so a status the Hub adds later is visible on sight instead of
  // quietly rendering as though it were normal.
  const intent = table[value];
  // Unknown values stay visible as themselves rather than rendering as normal,
  // and calm values stay quiet so the alarming ones carry the colour.
  const calm = CALM.has(value);
  return (
    <Tag intent={intent ?? "none"} minimal={calm || intent === undefined}>
      {value}
    </Tag>
  );
}

export const HealthTag = ({ value }: { value: string }) => statusTag(HEALTH_INTENT, value);
export const RolloutTag = ({ value }: { value: string }) => statusTag(ROLLOUT_INTENT, value);
export const ConfigTag = ({ value }: { value: string }) => statusTag(CONFIG_INTENT, value);
export const LifecycleTag = ({ value }: { value: string }) => statusTag(LIFECYCLE_INTENT, value);
