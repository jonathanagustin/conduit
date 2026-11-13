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

export const PROMOTION_LIFECYCLE_MESSAGE: Record<
  NonNullable<PromotionRecord["lifecycle"]>,
  string
> = {
  RUNNING: "Promotion in progress.",
  SUCCEEDED: "Promotion succeeded.",
  ABORTED: "Promotion aborted.",
  CANCELLED: "Promotion cancelled.",
};

export function isTerminalPromotion(lifecycle: string | undefined): boolean {
  return (
    lifecycle === "SUCCEEDED" ||
    lifecycle === "ABORTED" ||
    lifecycle === "CANCELLED"
  );
}
