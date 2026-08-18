import { Tag, type Intent } from "@blueprintjs/core";

/** One place where a Hub status string becomes a colour. Screens must not
 * decide this individually: the same status has to read the same everywhere,
 * and an unrecognised status must stay visible rather than silently render as
 * "normal". */
export const HEALTH_INTENT: Record<string, Intent> = {
  HEALTHY: "success",
  UNHEALTHY: "danger",
  INDETERMINATE: "warning",
  UNKNOWN: "none",
  ERROR: "danger",
  TERMINAL: "danger",
  WARNING: "warning",
  REPAIRING: "warning",
};

// The vocabularies below are the Hub's, from internal/environments/domain/
// reporting.go -- not a plausible guess. An earlier version invented values
// (IN_PROGRESS/FAILED rollout, PENDING/UNKNOWN config, RELINQUISHED/PENDING
// lifecycle) and, worse, omitted real ones, so a genuine BLOCKED or RECALLED
// rollout or an UNAVAILABLE config fell through the map to an unstyled "none"
// tag -- exactly the state an operator most needs coloured. types.ts already
// corrected the filter dropdown to these values; this colour table lagged it.
export const ROLLOUT_INTENT: Record<string, Intent> = {
  UP_TO_DATE: "success",
  IN_GRACE_PERIOD: "warning",
  OUT_OF_DATE: "warning",
  BLOCKED: "danger",
  RECALLED: "danger",
};

export const CONFIG_INTENT: Record<string, Intent> = {
  APPLIED: "success",
  DRIFTED: "warning",
  UNAVAILABLE: "warning",
  INDETERMINATE: "warning",
};

export const LIFECYCLE_INTENT: Record<string, Intent> = {
  INSTALLED: "success",
  UNINSTALLED: "none",
  UNKNOWN: "none",
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
