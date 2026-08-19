import { useState } from "react";
import {
  Button,
  Callout,
  H2,
  HTMLSelect,
  HTMLTable,
  InputGroup,
  Section,
  SectionCard,
  Tab,
  Tabs,
  Tag,
} from "@blueprintjs/core";

import {
  findingScore,
  severityTier,
  useVulnerabilityInventory,
  type SeverityTier,
} from "../../api/catalog.js";
import { recallRange, useRetirements, type Recall } from "../../api/risk.js";
import { EmptyState } from "../../components/EmptyState.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import { useIndexQuery } from "../useIndexQuery.js";
import { filterInventoryByTier, inventoryTiers, orderInventoryRows } from "./inventory.js";
import "./risk.css";

/** Risk Management -- two questions an operator asks about live risk. "What has
 * been withdrawn?" is Recalls, backed by GET /v1/catalog-retirements. "What are
 * my active vulnerabilities right now?" is the cross-Release inventory, which
 * the Hub serves per Release and this composes -- see useVulnerabilityInventory.
 * The inventory is a tab of its own so its per-Product fan-out fires only when
 * an operator opens it, not on every visit to the recalls list. */
export function RiskPage() {
  const [tab, setTab] = useState<string>("recalls");
  return (
    <div data-testid="risk-page">
      <ObjectHeader icon="shield" kind="Resource collection" title="Risk Management" />
      <Tabs id="risk-tabs" selectedTabId={tab} onChange={(next) => setTab(String(next))} renderActiveTabPanelOnly>
        <Tab id="recalls" title="Recalls" panel={<RecallsPanel />} />
        <Tab id="vulnerabilities" title="Vulnerabilities" panel={<VulnerabilityInventoryPanel />} />
      </Tabs>
    </div>
  );
}

/** The recalls that withdraw Release ranges from selection. Selecting a recall
 * reveals what it does to what is already installed, from the row already
 * loaded, so there is no second fetch: there is no per-recall endpoint, and
 * inventing one would promise a detail the Hub does not serve. */
function RecallsPanel() {
  const index = useIndexQuery();
  const { data, error, isPending, refetch } = useRetirements();
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const term = (index.query.search ?? "").toLowerCase();
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Recalls"
      onRetry={() => void refetch()}
    >
      {(recalls) => {
        const visible = term === "" ? recalls : recalls.filter((recall) => recallMatches(recall, term));
        const chosen = visible.find((recall) => recall.id === selected);
        return (
          <div data-testid="risk-recalls">
            <Section className="console-index" compact icon="shield" title="Recalls" titleRenderer={H2}>
              <SectionCard padded={false}>
                <form className="console-index-controls" role="search" onSubmit={index.applySearch}>
                  <InputGroup
                    leftIcon="search"
                    aria-label="Search recalls"
                    placeholder="Search Product, version, reason, or source"
                    value={index.search}
                    onValueChange={index.setSearch}
                  />
                  <Button type="submit" text="Search" />
                </form>
                {visible.length === 0 ? (
                  <EmptyState
                    icon="shield"
                    title={
                      index.query.search === undefined
                        ? "No Releases have been recalled."
                        : `No recall matches "${index.query.search}".`
                    }
                  />
                ) : (
                  <HTMLTable className="console-risk__table console-index-table" interactive striped>
                    <thead>
                      <tr>
                        <th scope="col">Product</th>
                        <th scope="col">Affected versions</th>
                        <th scope="col">Source</th>
                        <th scope="col">Status</th>
                      </tr>
                    </thead>
                    <tbody>
                      {visible.map((recall) => (
                        <tr
                          key={recall.id}
                          data-testid={`recall-${recall.id}`}
                          onClick={() => setSelected(recall.id === selected ? undefined : recall.id)}
                          aria-selected={recall.id === selected}
                        >
                          <td>{recall.product}</td>
                          <td>{recallRange(recall)}</td>
                          <td>
                            <SourceTag source={recall.source} />
                          </td>
                          <td>
                            {recall.reverted === true ? (
                              <Tag minimal round>
                                reverted
                              </Tag>
                            ) : (
                              <Tag intent="warning" round>
                                active
                              </Tag>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </HTMLTable>
                )}
                {chosen === undefined ? null : <RecallDetail recall={chosen} />}
              </SectionCard>
            </Section>
          </div>
        );
      }}
    </QueryState>
  );
}

/** The cross-Release inventory of active, unsuppressed, out-of-grace findings --
 * the Hub's own `activeFindings`, gathered across the catalog and ranked
 * worst-first by the score the Hub gives them. The severity filter is over the
 * tiers actually present, and a bounded catalog says so rather than implying the
 * first page is the whole of it. */
function VulnerabilityInventoryPanel() {
  const inventory = useVulnerabilityInventory();
  const [tier, setTier] = useState<SeverityTier | "ALL">("ALL");

  if (inventory.isPending) {
    return <p className="console-empty">Assembling the vulnerability inventory…</p>;
  }
  if (inventory.error !== null) {
    return (
      <Callout intent="danger" icon="error" data-testid="risk-vulnerabilities-error">
        {inventory.error instanceof Error
          ? inventory.error.message
          : "The vulnerability inventory could not be read."}
      </Callout>
    );
  }

  const ordered = orderInventoryRows(inventory.rows);
  const tiers = inventoryTiers(ordered);
  const selectedTier = tier === "ALL" ? undefined : tier;
  const visible = filterInventoryByTier(ordered, selectedTier);

  return (
    <div data-testid="risk-vulnerabilities">
      <Section
        className="console-index"
        compact
        icon="error"
        title="Active vulnerabilities"
        titleRenderer={H2}
        rightElement={
          <HTMLSelect
            aria-label="Severity"
            value={tier}
            onChange={(event) => setTier(event.currentTarget.value as SeverityTier | "ALL")}
          >
            <option value="ALL">All severities</option>
            {tiers.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </HTMLSelect>
        }
      >
        <SectionCard padded={false}>
          {inventory.bounded ? (
            <Callout
              compact
              icon="stacked-chart"
              intent="primary"
              data-testid="risk-vulnerabilities-bounded"
            >
              This covers the {inventory.productCount} Products on the first catalog page. The catalog
              holds more; open a Product to read its Releases' findings directly.
            </Callout>
          ) : null}
          {ordered.length === 0 ? (
            <EmptyState
              icon="shield"
              title="No active vulnerabilities across the catalog's Releases."
              description="Every current assessment is clean, suppressed, or within its grace period."
            />
          ) : visible.length === 0 ? (
            <EmptyState icon="filter" title={`No ${selectedTier} findings.`} />
          ) : (
            <HTMLTable className="console-risk__table console-index-table" striped>
              <thead>
                <tr>
                  <th scope="col">Severity</th>
                  <th scope="col">CVE</th>
                  <th scope="col">Product</th>
                  <th scope="col">Version</th>
                  <th scope="col">Hub score</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((row) => (
                  <tr
                    key={`${row.product}@${row.version}:${row.finding.id}`}
                    data-testid={`vuln-${row.finding.id}`}
                  >
                    <td>
                      <SeverityTag tier={severityTier(row.finding)} />
                    </td>
                    <td className="console-risk__cve">{row.finding.id}</td>
                    <td>{row.product}</td>
                    <td>{row.version}</td>
                    <td>{findingScore(row.finding).toFixed(1)}</td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
          )}
        </SectionCard>
      </Section>
    </div>
  );
}

/** The Hub's severity tier as a coloured tag. CRITICAL and HIGH are the tiers a
 * recall can be raised for, so they read danger; the rest step down. */
function SeverityTag({ tier }: { tier: SeverityTier }) {
  const intent =
    tier === "CRITICAL" || tier === "HIGH"
      ? "danger"
      : tier === "MEDIUM"
        ? "warning"
        : tier === "LOW"
          ? "primary"
          : "none";
  return (
    <Tag intent={intent} round minimal>
      {tier}
    </Tag>
  );
}

function recallMatches(recall: Recall, term: string): boolean {
  const haystacks = [
    recall.product,
    recall.startingVersion,
    recall.endingVersionExclusive ?? "",
    recall.reason,
    recall.source ?? "",
    recall.strategy.type,
    ...(recall.strategy.environments ?? []),
  ];
  return haystacks.some((value) => value.toLowerCase().includes(term));
}

function SourceTag({ source }: { source: string | undefined }) {
  if (source === "SECURITY") {
    // A vulnerability recall is the one a risk comparison may override; a manual
    // one is not, so the distinction is shown rather than flattened to "recall".
    return (
      <Tag intent="danger" round>
        SECURITY
      </Tag>
    );
  }
  if (source === "MANUAL") {
    return (
      <Tag intent="primary" round>
        MANUAL
      </Tag>
    );
  }
  return (
    <Tag minimal round>
      {source ?? "unspecified"}
    </Tag>
  );
}

function RecallDetail({ recall }: { recall: Recall }) {
  return (
    // A real Blueprint Section, not a bare <section>: the panel's white surface,
    // 1px hairline and 4px corner now come from the primitive
    // (--bp-surface-border-radius), matching the products detail panels, instead
    // of a border/radius rule on raw HTML. It stays inline and nonmodal -- the
    // selected row's context is preserved, so this is a primitive swap, not a
    // dialog.
    <Section
      className="console-risk__detail"
      compact
      title={`${recall.product} — ${recallRange(recall)}`}
      titleRenderer={H2}
      data-testid="recall-detail"
    >
      <SectionCard>
      <dl className="console-risk__meta">
        <div>
          <dt>Reason</dt>
          <dd>{recall.reason === "" ? "—" : recall.reason}</dd>
        </div>
        <div>
          <dt>Strategy</dt>
          <dd>{describeStrategy(recall)}</dd>
        </div>
        {recall.strategy.environments !== undefined && recall.strategy.environments.length > 0 ? (
          <div>
            <dt>Environments</dt>
            <dd>{recall.strategy.environments.join(", ")}</dd>
          </div>
        ) : null}
        {recall.strategy.freezeUntil !== undefined && recall.strategy.freezeUntil !== "" ? (
          <div>
            <dt>Frozen until</dt>
            <dd>
              <Timestamp value={recall.strategy.freezeUntil} />
            </dd>
          </div>
        ) : null}
        {recall.excludedVersions !== undefined && recall.excludedVersions.length > 0 ? (
          <div>
            <dt>Excluded versions</dt>
            <dd>{recall.excludedVersions.join(", ")}</dd>
          </div>
        ) : null}
        {recall.assessmentRunId !== undefined && recall.assessmentRunId !== "" ? (
          <div>
            <dt>Assessment run</dt>
            <dd>{recall.assessmentRunId}</dd>
          </div>
        ) : null}
      </dl>
      </SectionCard>
    </Section>
  );
}

function describeStrategy(recall: Recall): string {
  const minimum = recall.strategy.minimumVersion;
  switch (recall.strategy.type) {
    case "ROLL_FORWARD":
    case "":
      return minimum !== undefined && minimum !== ""
        ? `Roll forward to at least ${minimum}`
        : "Roll forward off the recalled range";
    case "ALLOW_DOWNGRADE":
      return minimum !== undefined && minimum !== ""
        ? `Allow downgrade to at least ${minimum}`
        : "Allow downgrade";
    case "FREEZE":
      return "Freeze on the current version";
    case "SELECTED_ENVIRONMENTS":
      return "Applies to selected Environments only";
    default:
      return recall.strategy.type;
  }
}
