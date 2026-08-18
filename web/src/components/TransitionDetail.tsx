import { Callout, Tag } from "@blueprintjs/core";

import type { PromotionRecord } from "../api/catalog.js";
import "./transition-detail.css";

/** What governs one transition. This is the panel capture 314 describes.
 *
 * It lived in `features/promotions/PromotionsPage.tsx` and was imported from
 * `features/products/ProductsPage.tsx`, which is the shape of a shared
 * presentation primitive rather than of a screen: two features render the same
 * panel from the same `PromotionRecord`. Both callers now import it from here,
 * so `verify-console-boundaries.mjs` can forbid one feature importing another
 * without forbidding this. */
export function TransitionDetail({ promotion }: { promotion: PromotionRecord }) {
  const blockers = promotion.blockers ?? [];
  const reasons = promotion.reasons ?? [];
  return (
    <div
      className="console-transition"
      role="region"
      aria-label={`${promotion.fromChannel} to ${promotion.toChannel} promotion constraints`}
      data-testid={`promotion-constraints-${promotion.id}`}
    >
      <h3>
        {promotion.fromChannel} &rarr; {promotion.toChannel}{" "}
        <Tag intent={promotion.status === "PASSED" ? "success" : "warning"} round>
          {promotion.status}
        </Tag>{" "}
        <Tag minimal>version {promotion.version}</Tag>
      </h3>

      {promotion.status === "PASSED" ? (
        <Callout intent="success" icon="tick-circle" compact>
          Every constraint on this transition is satisfied.
        </Callout>
      ) : blockers.length === 0 && reasons.length === 0 ? (
        <Callout intent="warning" compact>
          This promotion is blocked, but the Hub gave no reason. That is a Hub defect rather than a
          constraint you can satisfy.
        </Callout>
      ) : blockers.length === 0 ? (
        // Reasons is derived from Blockers, so it is only shown when a Hub
        // predates the structured form; rendering both says everything twice.
        <ul className="console-transition__blockers">
          {reasons.map((reason, index) => (
            <li key={index}>{reason}</li>
          ))}
        </ul>
      ) : (
        <ul className="console-transition__blockers">
          {blockers.map((blocker, index) => (
            <li key={`${blocker.kind} ${index}`}>
              <Tag minimal round>
                {blocker.kind}
              </Tag>{" "}
              {blocker.message}
              {(blocker.subjects ?? []).length > 0 ? (
                <span className="console-transition__subjects">
                  {" "}
                  ({(blocker.subjects ?? []).join(", ")})
                </span>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
