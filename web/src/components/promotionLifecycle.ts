import type { Intent } from "@blueprintjs/core";
import type { PromotionRecord } from "../api/catalog.js";

// Matched against the Go lifecycle vocabulary by verify-console-contract.mjs.
export const PROMOTION_LIFECYCLE_INTENT: Record<
  NonNullable<PromotionRecord["lifecycle"]>,
  Intent
> = {
  RUNNING: "primary",
  SUCCEEDED: "success",
  ABORTED: "danger",
  CANCELLED: "warning",
};
