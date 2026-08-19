import { useState } from "react";
import { Button, H2, HTMLTable, InputGroup, Section, SectionCard, Tag } from "@blueprintjs/core";
import { Link, useParams } from "react-router-dom";

import {
  isZeroTime,
  orderFindingsByHubScore,
  useProductReleases,
  useProducts,
  usePromotions,
} from "../../api/catalog.js";
import type { Release, VulnerabilityAssessment } from "../../api/catalog.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import { ReleaseChannelGraph } from "../../components/ReleaseChannelGraph.js";
import { TransitionDetail } from "../../components/TransitionDetail.js";
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
  // Which Release's findings are revealed. Held here rather than in the URL
  // because it selects within data this page has already loaded, the same way
  // the Import and Export screens reveal a row's detail.
  const [selectedRelease, setSelectedRelease] = useState<string | undefined>(undefined);
  const chosenRelease = (data ?? []).find((release) => release.version === selectedRelease);
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
          {/* The Product view as the 2020 Apollo capture composes it: a fixed
              resource summary beside the release/change feed and the dense
              Release table (docs/ui/HISTORICAL-APOLLO-EVIDENCE-2020-2022.md).
              The grid is the fixed-rail + fluid-work equation; the pane holds
              the summary, the work column the pipeline, table and any revealed
              finding. Below 900px the rail collapses above the work column. */}
          <div className="console-products__browser">
            <div className="console-products__pane">
              <ProductResourceSummary product={product} releases={releases} />
            </div>
            <div className="console-products__work" data-testid="product-work">
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
                      {(release.vulnerabilityAssessment?.activeFindings ?? []).length === 0 &&
                      (release.vulnerabilityAssessment?.suppressedFindingDetails ?? []).length === 0 ? (
                        <AssessmentTag release={release} />
                      ) : (
                        <Button
                          minimal
                          small
                          aria-pressed={selectedRelease === release.version}
                          data-testid={`release-assessment-${release.version}`}
                          onClick={() =>
                            setSelectedRelease(
                              selectedRelease === release.version ? undefined : release.version,
                            )
                          }
                        >
                          <AssessmentTag release={release} />
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
                </HTMLTable>
              )}
              {chosenRelease?.vulnerabilityAssessment === undefined ? null : (
                <VulnerabilityDetail
                  version={chosenRelease.version}
                  assessment={chosenRelease.vulnerabilityAssessment}
                />
              )}
            </div>
          </div>
        </div>
      )}
    </QueryState>
  );
}

/** The per-CVE detail behind a Release's assessment tag.
 *
 * Every field here is already in the payload the table above rendered -- the
 * Hub serializes `catalog.Release` whole on `GET /v1/products/{product}/releases`
 * -- so revealing it costs no second request, the same rule the Import, Export
 * and Risk screens follow. Until this existed the console fetched the per-CVE
 * findings and the whole suppression approval trail on every page load and drew
 * one tag and one integer from them.
 *
 * Findings are ordered by the score the Hub would give them -- `findingScore`
 * mirrored in `orderFindingsByHubScore` -- so the panel's order is the order
 * `ReleaseRiskScore` is built from. An earlier revision ordered on CVSS alone,
 * which put every severity-only finding at the bottom and so inverted the Hub
 * for the case that matters most: a CRITICAL finding reporting no CVSS scores
 * 9.0 upstream and rendered below one scoring 0.5.
 *
 * Severity is printed rather than coloured, which is a separate decision and
 * still stands: `severity` is an unconstrained string with no Go const set, so
 * a colour map keyed on it could not be guarded by
 * `verify-console-contract.mjs`. Ordering by a numeric mirror of the Hub's own
 * function is not the same thing as inventing a status vocabulary. */
function VulnerabilityDetail({
  version,
  assessment,
}: {
  version: string;
  assessment: VulnerabilityAssessment;
}) {
  const active = orderFindingsByHubScore(assessment.activeFindings ?? []);
  const suppressed = assessment.suppressedFindingDetails ?? [];
  return (
    <Section
      compact
      icon="shield"
      title={`Vulnerability findings for ${version}`}
      titleRenderer={H2}
      className="console-products__assessment"
      data-testid="release-assessment-detail"
    >
      <SectionCard>
      <p className="console-products__assessment-provenance">
        {assessment.provider === undefined ? "Scanner not named" : `Scanned by ${assessment.provider}`}
        {isZeroTime(assessment.assessedAt) ? null : (
          <>
            {" "}
            &middot; <Timestamp value={assessment.assessedAt} />
          </>
        )}
        {assessment.gracePeriodFindings === undefined || assessment.gracePeriodFindings === 0 ? null : (
          <>
            {" "}
            &middot; {assessment.gracePeriodFindings} within a grace period
          </>
        )}
      </p>

      {active.length === 0 ? (
        <p className="console-empty">No active findings.</p>
      ) : (
        <>
          {/* Without this the order looks wrong: a finding whose CVSS column
              reads "—" can outrank one reading 4.2, because the Hub scores an
              unreported CVSS from its severity instead. */}
          <p className="console-products__assessment-provenance">
            Worst first, by the score the Hub assigns: CVSS when it was reported,
            otherwise the reported severity.
          </p>
          <HTMLTable className="console-products__table" striped data-testid="release-active-findings">
          <thead>
            <tr>
              <th scope="col">CVE</th>
              <th scope="col">CVSS</th>
              <th scope="col">Severity (as reported)</th>
            </tr>
          </thead>
          <tbody>
            {active.map((finding) => (
              <tr key={finding.id} data-testid={`finding-${finding.id}`}>
                <td>{finding.id}</td>
                {/* An absent CVSS is shown as absent. Rendering it as 0 would
                    read as harmless, which is the opposite of unknown. */}
                <td>{finding.cvss === undefined ? "—" : finding.cvss.toFixed(1)}</td>
                <td>{finding.severity ?? "—"}</td>
              </tr>
            ))}
            </tbody>
          </HTMLTable>
        </>
      )}

      {suppressed.length === 0 ? null : (
        <>
          <h3>Suppressed</h3>
          {/* A suppressed count is not evidence on its own: the Hub refuses a
              suppression that cannot name an approver, a Change Request and an
              unexpired window, so those are what make the exclusion auditable
              and what is shown. */}
          <HTMLTable className="console-products__table" striped data-testid="release-suppressed-findings">
            <thead>
              <tr>
                <th scope="col">CVE</th>
                <th scope="col">Category</th>
                <th scope="col">Approved by</th>
                <th scope="col">Change Request</th>
                <th scope="col">Valid until</th>
              </tr>
            </thead>
            <tbody>
              {suppressed.map((finding) => (
                <tr key={finding.findingId} data-testid={`suppressed-${finding.vulnerabilityId}`}>
                  <td>{finding.vulnerabilityId}</td>
                  <td>{finding.suppression?.category ?? "—"}</td>
                  <td>{finding.suppression?.approvedBy ?? "—"}</td>
                  <td>{finding.suppression?.changeRequestId ?? "—"}</td>
                  <td>
                    {isZeroTime(finding.suppression?.validUntil) ? (
                      "—"
                    ) : (
                      <Timestamp value={finding.suppression?.validUntil} />
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </HTMLTable>
        </>
      )}
      </SectionCard>
    </Section>
  );
}

/** The fixed resource summary that heads the Product browser, the counterpart
 * of the 2020 Apollo capture's "fixed resource summary" beside the release feed
 * and install table. Every fact is derived from the Releases the page already
 * loaded -- distinct channels, how many Releases carry an assessment and how
 * many report active findings -- so the pane cannot disagree with the table it
 * sits beside, the same rule the Environment Overview summary follows. Channels
 * are the Product's own vocabulary, so they are shown as Tags rather than
 * counted into a status the Hub never named. */
function ProductResourceSummary({
  product,
  releases,
}: {
  product: string;
  releases: readonly Release[];
}) {
  const channels = [...new Set(releases.flatMap((release) => release.channels ?? []))].sort();
  const assessed = releases.filter((release) => release.vulnerabilityAssessment !== undefined).length;
  const withActive = releases.filter(
    (release) => (release.vulnerabilityAssessment?.activeVulnerabilities ?? 0) > 0,
  ).length;
  return (
    <Section
      compact
      icon="cube"
      title="Resource"
      titleRenderer={H2}
      className="console-products__summary"
      data-testid="product-resource-summary"
    >
      <SectionCard>
        <dl className="console-products__facts">
          <div>
            <dt>Product</dt>
            {/* A coordinate is longer than the pane and never wrapped, so it
                truncates and carries its full value as a title. */}
            <dd className="console-products__coordinate" title={product}>
              {product}
            </dd>
          </div>
          <div>
            <dt>Releases</dt>
            <dd>{releases.length}</dd>
          </div>
          <div>
            <dt>Channels</dt>
            <dd>
              {channels.length === 0
                ? "—"
                : channels.map((channel) => (
                    <Tag key={channel} minimal round>
                      {channel}
                    </Tag>
                  ))}
            </dd>
          </div>
          <div>
            <dt>Assessed</dt>
            <dd>
              {assessed} of {releases.length}
              {withActive > 0 ? (
                <>
                  {" "}
                  &middot;{" "}
                  <Tag intent="warning" minimal round>
                    {withActive} with active findings
                  </Tag>
                </>
              ) : null}
            </dd>
          </div>
        </dl>
      </SectionCard>
    </Section>
  );
}

function ProductPromotionPipeline({ product }: { product: string }) {
  const { data, error, isPending, refetch } = usePromotions({ product, limit: 200 });
  const [selected, setSelected] = useState<string | undefined>(undefined);
  return (
    <Section
      compact
      icon="exchange"
      title="Release Channel pipeline"
      titleRenderer={H2}
      className="console-products__pipeline"
    >
      <SectionCard>
      <QueryState
        isPending={isPending}
        error={error}
        data={data}
        subject={`promotion pipeline for Product ${product}`}
        onRetry={() => void refetch()}
        headingLevel={2}
      >
        {(page) => {
          const promotions = page.promotions;
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
      </SectionCard>
    </Section>
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
