import { useState } from "react";
import { Button, Callout, InputGroup, Section, SectionCard } from "@blueprintjs/core";

import { usePromotions, type PromotionRecord } from "../../api/catalog.js";
import { QueryState } from "../../components/QueryState.js";
import { ReleaseChannelGraph } from "../../components/ReleaseChannelGraph.js";
import { TransitionDetail } from "../../components/TransitionDetail.js";
import { IndexPagination } from "../../components/IndexPagination.js";
import { useIndexQuery } from "../useIndexQuery.js";
import "./promotions.css";

/** Promotion -- P0: "Can this Release advance, and why?"
 *
 * Grouped by Product, because that is where the evidence puts promotion: V1
 * capture 337 shows the pipeline on the Product overview page, and 306 shows a
 * Release's progress along it. Each group renders the channel run with a
 * selectable transition, and selecting one shows its constraints -- V1 314.
 *
 * The page itself is a Conduit extension and says so below: a fleet-wide
 * promotions queue is useful for an operator asking "what is stuck anywhere",
 * but upstream has no such screen, and presenting it as the Apollo pipeline
 * would teach a model of the product that does not exist. */
export function PromotionsPage() {
  const index = useIndexQuery();
  const { data, error, isPending, refetch } = usePromotions(index.query);
  const [selected, setSelected] = useState<string | undefined>(undefined);

  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="promotions"
      onRetry={() => void refetch()}
    >
      {(page) => {
        const promotions = page.promotions;
        const byProduct = new Map<string, PromotionRecord[]>();
        for (const promotion of promotions) {
          const group = byProduct.get(promotion.product) ?? [];
          group.push(promotion);
          byProduct.set(promotion.product, group);
        }
        const chosen = promotions.find((promotion) => promotion.id === selected);

        return (
          <div className="console-promotions" data-testid="promotions-page">
            <Callout icon="info-sign" compact className="console-promotions__scope">
              This fleet-wide view is a Conduit extension. Upstream reaches promotion through a
              Product, where the pipeline belongs.
            </Callout>
            <form className="console-index-controls" role="search" onSubmit={index.applySearch}>
              <InputGroup
                leftIcon="search"
                aria-label="Search Promotions"
                placeholder="Search Product, version, channel, or status"
                value={index.search}
                onValueChange={index.setSearch}
              />
              <Button type="submit" text="Search" />
            </form>

            {promotions.length === 0 ? (
              <p className="console-empty">
                {index.query.search === undefined
                  ? "No promotions have been evaluated."
                  : `No promotion matches "${index.query.search}".`}
              </p>
            ) : (
              [...byProduct.entries()].map(([product, group]) => (
                <Section key={product} className="console-promotions__card" compact title={product}>
                  <SectionCard padded>
                    <ReleaseChannelGraph
                      promotions={group}
                      selectedId={selected}
                      onSelect={(id) => setSelected(id === selected ? undefined : id)}
                    />
                    {chosen !== undefined && group.some((entry) => entry.id === chosen.id) ? (
                      <TransitionDetail promotion={chosen} />
                    ) : (
                      <p className="console-empty">
                        Select a transition to see what governs it.
                      </p>
                    )}
                  </SectionCard>
                </Section>
              ))
            )}
            <IndexPagination
              page={page}
              total={page.total}
              testId="promotions-pagination"
              onMove={index.moveToCursor}
            />
          </div>
        );
      }}
    </QueryState>
  );
}
