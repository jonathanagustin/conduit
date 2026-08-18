import { Tag } from "@blueprintjs/core";

import type { PromotionRecord } from "../api/catalog.js";
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
  return (
    <ol
      className="console-graph"
      aria-label="Release Channel pipeline"
      data-testid="release-channel-pipeline"
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
                aria-label={`${outgoing.fromChannel} to ${outgoing.toChannel}: ${outgoing.status}`}
                data-testid={`promotion-transition-${outgoing.id}`}
                onClick={() => onSelect(outgoing.id)}
              >
                <span aria-hidden="true" className="console-graph__arrow">
                  &rarr;
                </span>
                <Tag
                  intent={outgoing.status === "PASSED" ? "success" : "warning"}
                  round
                  minimal={outgoing.status === "PASSED"}
                >
                  {outgoing.status === "PASSED"
                    ? "can advance"
                    : `${(outgoing.blockers ?? outgoing.reasons ?? []).length || 1} blocking`}
                </Tag>
              </button>
            ) : null}
          </li>
        );
      })}
    </ol>
  );
}

/** The most recent evaluation leaving a channel. */
export function latestForChannel(
  promotions: PromotionRecord[],
  channel: string,
): PromotionRecord | undefined {
  return promotions
    .filter((promotion) => promotion.fromChannel === channel)
    .reduce<PromotionRecord | undefined>((latest, candidate) => {
      if (latest === undefined) {
        return candidate;
      }
      // Missing timestamps sort oldest, so a record that carries one always
      // wins over one that does not.
      return (candidate.evaluatedAt ?? candidate.evaluationStartedAt ?? "") >=
        (latest.evaluatedAt ?? latest.evaluationStartedAt ?? "")
        ? candidate
        : latest;
    }, undefined);
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
    return [...new Set(promotions.flatMap((p) => [p.fromChannel, p.toChannel]))];
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
