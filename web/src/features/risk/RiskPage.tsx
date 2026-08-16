import { useState } from "react";
import { Button, H2, HTMLTable, InputGroup, Section, SectionCard, Tag } from "@blueprintjs/core";

import { recallRange, useRetirements, type Recall } from "../../api/risk.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import { useIndexQuery } from "../useIndexQuery.js";
import "./risk.css";

/** Risk Management -- the recalls that withdraw Release ranges from selection.
 * GET /v1/catalog-retirements returns the whole set as one named array, so the
 * search filters locally. Selecting a recall reveals what it does to what is
 * already installed, from the row already loaded, so there is no second fetch:
 * there is no per-recall endpoint, and inventing one would promise a detail the
 * Hub does not serve. */
export function RiskPage() {
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
          <div data-testid="risk-page">
            <ObjectHeader icon="shield" kind="Resource collection" title="Risk Management" />
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
                  <p className="console-empty">
                    {index.query.search === undefined
                      ? "No Releases have been recalled."
                      : `No recall matches "${index.query.search}".`}
                  </p>
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
    <section className="console-risk__detail" aria-labelledby="recall-detail-title" data-testid="recall-detail">
      <h2 id="recall-detail-title">
        {recall.product} — {recallRange(recall)}
      </h2>
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
    </section>
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
