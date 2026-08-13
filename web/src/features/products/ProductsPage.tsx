import { useState } from "react";
import { Button, H2, HTMLTable, InputGroup, Section, SectionCard, Tag } from "@blueprintjs/core";
import { Link, useParams } from "react-router-dom";

import {
  useProductReleases,
  useProducts,
  usePromotions,
} from "../../api/catalog.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { ReleaseChannelGraph } from "../promotions/ReleaseChannelGraph.js";
import { TransitionDetail } from "../promotions/PromotionsPage.js";
import { IndexPagination } from "../../components/IndexPagination.js";
import { useIndexQuery } from "../useIndexQuery.js";
import "./products.css";

/** Product index and detail -- P0: "What releases exist and where are they
 * running?" Capture 043 is the Product catalog, 044 a Product page showing
 * three Releases. */
export function ProductsPage() {
  const index = useIndexQuery();
  const { data, error, isPending, refetch } = useProducts(index.query);
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Products"
      onRetry={() => void refetch()}
    >
      {(page) => (
        <div data-testid="products-page">
            <ObjectHeader icon="cube" kind="Resource collection" title="Software Catalog" />
            <Section className="console-index" compact icon="cube" title="Visible Products" titleRenderer={H2}>
              <SectionCard padded={false}>
                <form className="console-index-controls" role="search" onSubmit={index.applySearch}>
                  <InputGroup
                    leftIcon="search"
                    aria-label="Search Products"
                    placeholder="Search Product coordinate"
                    value={index.search}
                    onValueChange={index.setSearch}
                  />
                  <Button type="submit" text="Search" />
                </form>
                {page.products.length === 0 ? (
                  <p className="console-empty">
                    {index.query.search === undefined
                      ? "No Products have been published."
                      : `No Product matches "${index.query.search}".`}
                  </p>
                ) : (
                  <>
            <HTMLTable className="console-products__table console-index-table" interactive striped>
              <thead>
                <tr>
                  <th scope="col">Product</th>
                  <th scope="col">Releases</th>
                  <th scope="col">Latest</th>
                </tr>
              </thead>
              <tbody>
                {page.products.map((product) => (
                  <tr key={product.product}>
                    <td>
                      <Link to={`/products/${encodeURIComponent(product.product)}`}>
                        {product.product}
                      </Link>
                    </td>
                    <td>{product.releaseCount}</td>
                    <td>{product.latestVersion ?? "—"}</td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
                  <div className="console-index-mobile-records" data-testid="product-mobile-records">
                    {page.products.map((product) => (
                      <dl className="console-index-mobile-record" key={product.product}>
                        <div><dt>Product</dt><dd><Link to={`/products/${encodeURIComponent(product.product)}`}>{product.product}</Link></dd></div>
                        <div><dt>Releases</dt><dd>{product.releaseCount}</dd></div>
                        <div><dt>Latest</dt><dd>{product.latestVersion ?? "—"}</dd></div>
                      </dl>
                    ))}
                  </div>
                  </>
                )}
                <IndexPagination
                  page={page}
                  total={page.total}
                  testId="products-pagination"
                  onMove={index.moveToCursor}
                />
              </SectionCard>
            </Section>
          </div>
      )}
    </QueryState>
  );
}

export function ProductDetailPage() {
  const { product = "" } = useParams();
  const { data, error, isPending, refetch } = useProductReleases(product);
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject={`Product ${product}`}
      onRetry={() => void refetch()}
    >
      {(releases) => (
        <div className="console-products" data-testid="product-detail">
          <ObjectHeader
            icon="cube"
            kind="Product"
            title={product}
            subtitle={`${releases.length} ${releases.length === 1 ? "Release" : "Releases"}`}
          />
          <ProductPromotionPipeline product={product} />
          {releases.length === 0 ? (
            <p className="console-empty">This Product has no Releases.</p>
          ) : (
            <HTMLTable className="console-products__table" striped>
              <thead>
                <tr>
                  <th scope="col">Version</th>
                  <th scope="col">Channels</th>
                  <th scope="col">Chart digest</th>
                  <th scope="col">Vulnerability assessment</th>
                </tr>
              </thead>
              <tbody>
                {releases.map((release) => (
                  <tr key={release.version} data-testid={`release-${release.version}`}>
                    <td>{release.version}</td>
                    <td>
                      {(release.channels ?? []).length === 0
                        ? "—"
                        : (release.channels ?? []).map((channel) => (
                            <Tag key={channel} minimal round>
                              {channel}
                            </Tag>
                          ))}
                    </td>
                    {/* The digest is the coordinate that makes a Release
                        immutable, so it is shown rather than the tag. */}
                    <td className="console-products__digest">{release.chart.digest}</td>
                    <td>
                      <AssessmentTag release={release} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
          )}
        </div>
      )}
    </QueryState>
  );
}

function ProductPromotionPipeline({ product }: { product: string }) {
  const { data, error, isPending, refetch } = usePromotions(product);
  const [selected, setSelected] = useState<string | undefined>(undefined);
  return (
    <section className="console-products__pipeline" aria-labelledby="product-promotion-title">
      <h2 id="product-promotion-title">Release Channel pipeline</h2>
      <QueryState
        isPending={isPending}
        error={error}
        data={data}
        subject={`promotion pipeline for Product ${product}`}
        onRetry={() => void refetch()}
        headingLevel={2}
      >
        {(promotions) => {
          const chosen = promotions.find((promotion) => promotion.id === selected);
          return promotions.length === 0 ? (
            <p className="console-empty">
              No promotion evaluations are available for this Product, so its channel pipeline is
              unknown.
            </p>
          ) : (
            <>
              <ReleaseChannelGraph
                promotions={promotions}
                selectedId={selected}
                onSelect={(id) => setSelected(id === selected ? undefined : id)}
              />
              {chosen === undefined ? (
                <p className="console-empty">Select a transition to see what governs it.</p>
              ) : (
                <TransitionDetail promotion={chosen} />
              )}
            </>
          );
        }}
      </QueryState>
    </section>
  );
}

function AssessmentTag({
  release,
}: {
  release: { vulnerabilityAssessment?: { outcome?: string; activeVulnerabilities?: number } };
}) {
  const assessment = release.vulnerabilityAssessment;
  if (assessment === undefined) {
    // Not scanned is not the same as scanned and clean, and must not read as it.
    return (
      <Tag minimal round>
        not assessed
      </Tag>
    );
  }
  const active = assessment.activeVulnerabilities ?? 0;
  return (
    <Tag intent={assessment.outcome === "PASS" ? "success" : "danger"} round>
      {assessment.outcome ?? "UNKNOWN"}
      {active > 0 ? ` (${active} active)` : ""}
    </Tag>
  );
}
