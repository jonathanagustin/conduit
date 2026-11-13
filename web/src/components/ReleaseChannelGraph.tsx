import { Tag } from "@blueprintjs/core";

import type { PromotionRecord } from "../api/catalog.js";
import {
  PROMOTION_LIFECYCLE_INTENT,
  isTerminalPromotion,
} from "./promotionLifecycle.js";
import "./release-channel-graph.css";

/** The horizontal Release Channel graph: an ordered run of channels with a
 * selectable transition between each pair.
 *
 * This is the composition the evidence establishes rather than one this console
 * chose. V1 capture 337 is "the Release promotion pipeline that all Product
 * Releases will follow, on the Product overview page"; 306 is that pipeline's
 * progress and results on a Release Overview; and 314 is "select a transition
 * node for information regarding promotion constraints for the promotion
 * between RELEASE and STAGING Release Channels". The previous rendering -- a
 * flat list of transition cards -- carried the same data and taught the wrong
 * model, because it never showed a channel's position in the run.
 *
 * Semantics before decoration: this is an ordered list, and the transitions are
 * buttons, so the order survives without CSS and the constraints are reachable
 * by keyboard. */
export function ReleaseChannelGraph({
  promotions,
  selectedId,
  onSelect,
}: {
  promotions: PromotionRecord[];
  selectedId: string | undefined;
  onSelect: (id: string) => void;
}) {
  const channels = orderChannels(promotions);
  const shown = new Set(
    channels.flatMap((channel) => {
      const current = latestForChannel(promotions, channel);
      return current ? [current.id] : [];
    }),
  );
  const otherActive = currentInProgress(promotions).filter(
    (promotion) => !shown.has(promotion.id),
  );
  return (
    <div data-testid="release-channel-pipeline">
      <ol
        className="console-graph"
        aria-label="Release Channel pipeline"
      >
        {channels.map((channel, index) => {
          // Promotion records are immutable evidence and a boundary accumulates
          // them, so find() would surface whichever the Hub happened to sort
          // first -- an older evaluation implying a blocking state that has since
          // been resolved. The most recent evaluation is the current answer.
          const outgoing = latestForChannel(promotions, channel);
          return (
            <li key={channel} className="console-graph__node">
              <span className="console-graph__channel">{channel}</span>
              {outgoing !== undefined && index < channels.length - 1 ? (
                <button
                  type="button"
                  className={
                    outgoing.id === selectedId
                      ? "console-graph__transition console-graph__transition--selected"
                      : "console-graph__transition"
                  }
                  aria-pressed={outgoing.id === selectedId}
                  aria-label={`${outgoing.fromChannel} to ${outgoing.toChannel}: ${outgoing.lifecycle ?? outgoing.status}${outgoing.lifecycleReason ? `: ${outgoing.lifecycleReason}` : ""}`}
                  data-testid={`promotion-transition-${outgoing.id}`}
                  onClick={() => onSelect(outgoing.id)}
                >
                  <span aria-hidden="true" className="console-graph__arrow">
                    &rarr;
                  </span>
                  <Tag
                    intent={
                      outgoing.lifecycle
                        ? PROMOTION_LIFECYCLE_INTENT[outgoing.lifecycle]
                        : outgoing.status === "PASSED"
                          ? "success"
                          : "warning"
                    }
                    round
                    minimal={outgoing.status === "PASSED"}
                  >
                    {outgoing.lifecycle ??
                      (outgoing.status === "PASSED"
                        ? "can advance"
                        : `${(outgoing.blockers ?? outgoing.reasons ?? []).length || 1} blocking`)}
                  </Tag>
                  {outgoing.lifecycleReason ? (
                    <span>{outgoing.lifecycleReason}</span>
                  ) : null}
                </button>
              ) : null}
            </li>
          );
        })}
      </ol>
      {otherActive.length > 0 ? (
        <div role="group" aria-label="Other in-progress promotions">
          <p>Other in-progress promotions</p>
          {otherActive.map((promotion) => (
            <button
              key={promotion.id}
              type="button"
              className="console-graph__transition"
              aria-pressed={promotion.id === selectedId}
              data-testid={`promotion-transition-${promotion.id}`}
              onClick={() => onSelect(promotion.id)}
            >
              version {promotion.version}: {promotion.fromChannel} &rarr;{" "}
              {promotion.toChannel}{" "}
              <Tag intent="primary" round>
                RUNNING
              </Tag>
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

/** The most recent evaluation leaving a channel. */
export function latestForChannel(
  promotions: PromotionRecord[],
  channel: string,
): PromotionRecord | undefined {
  return promotions
    .filter((promotion) => promotion.fromChannel === channel)
    .reduce<PromotionRecord | undefined>(newerEvaluation, undefined);
}

function newerEvaluation(
  latest: PromotionRecord | undefined,
  candidate: PromotionRecord,
): PromotionRecord {
  if (!latest) return candidate;
  if (latest.runId && latest.runId === candidate.runId) {
    const latestTerminal = isTerminalPromotion(latest.lifecycle);
    const candidateTerminal = isTerminalPromotion(candidate.lifecycle);
    if (latestTerminal && !candidateTerminal) return latest;
    if (candidateTerminal && !latestTerminal) return candidate;
    if (candidate.previousEvaluationId === latest.id) return candidate;
    if (latest.previousEvaluationId === candidate.id) return latest;
  }
  return (candidate.evaluatedAt ?? candidate.evaluationStartedAt ?? "") >=
    (latest.evaluatedAt ?? latest.evaluationStartedAt ?? "")
    ? candidate
    : latest;
}

// Keep active Release versions discoverable even when another version owns the
// most recent channel evaluation. A terminal head removes its older RUNNING
// observations instead of leaving a stopped run in this view.
export function currentInProgress(
  promotions: PromotionRecord[],
): PromotionRecord[] {
  const heads = new Map<string, PromotionRecord>();
  for (const promotion of promotions) {
    const key = promotion.runId ?? promotion.id;
    heads.set(key, newerEvaluation(heads.get(key), promotion));
  }
  return [...heads.values()].filter(
    (promotion) => promotion.lifecycle === "RUNNING",
  );
}

/** Channels in the order the promotions chain them, not alphabetically.
 *
 * The Hub publishes transitions, not a pipeline: the order has to be recovered
 * by following fromChannel to toChannel. A channel nobody promotes out of is
 * the end of the run, and one nobody promotes into is the start. Cycles are
 * possible in a misconfigured Product, so the walk is bounded by the number of
 * channels rather than by reaching the end. */
export function orderChannels(promotions: PromotionRecord[]): string[] {
  const next = new Map<string, string>();
  const targets = new Set<string>();
  for (const promotion of promotions) {
    next.set(promotion.fromChannel, promotion.toChannel);
    targets.add(promotion.toChannel);
  }
  const start = [...next.keys()].find((channel) => !targets.has(channel));
  if (start === undefined) {
    // No unambiguous head: either there are no promotions, or they form a
    // cycle. Show what exists rather than nothing.
    return [
      ...new Set(promotions.flatMap((p) => [p.fromChannel, p.toChannel])),
    ];
  }
  const ordered = [start];
  const limit = next.size + 1;
  for (let step = 0; step < limit; step += 1) {
    const last = ordered[ordered.length - 1];
    const following = last === undefined ? undefined : next.get(last);
    if (following === undefined || ordered.includes(following)) {
      break;
    }
    ordered.push(following);
  }
  return ordered;
}
