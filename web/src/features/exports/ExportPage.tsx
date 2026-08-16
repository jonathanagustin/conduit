import { useState } from "react";
import { Button, H2, HTMLTable, InputGroup, Section, SectionCard, Tag } from "@blueprintjs/core";

import {
  pipelineGuarantees,
  pipelineStatus,
  pipelineStatusIntent,
  useExportPipelines,
  type ExportPipeline,
} from "../../api/exports.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { useIndexQuery } from "../useIndexQuery.js";
import "./exports.css";

/** Export -- the pipelines that build offline Bundles. GET /v1/export-pipelines
 * returns the whole set as one named array, so the search filters locally.
 * Selecting a pipeline reveals what it selects and the gates a Release must
 * pass, from the row already loaded; there is no build-history endpoint, so
 * the screen does not pretend to show one. */
export function ExportPage() {
  const index = useIndexQuery();
  const { data, error, isPending, refetch } = useExportPipelines();
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const term = (index.query.search ?? "").toLowerCase();
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Export pipelines"
      onRetry={() => void refetch()}
    >
      {(pipelines) => {
        const visible = term === "" ? pipelines : pipelines.filter((pipeline) => pipelineMatches(pipeline, term));
        const chosen = visible.find((pipeline) => pipeline.id === selected);
        return (
          <div data-testid="export-page">
            <ObjectHeader icon="export" kind="Resource collection" title="Export" />
            <Section className="console-index" compact icon="export" title="Export pipelines" titleRenderer={H2}>
              <SectionCard padded={false}>
                <form className="console-index-controls" role="search" onSubmit={index.applySearch}>
                  <InputGroup
                    leftIcon="search"
                    aria-label="Search export pipelines"
                    placeholder="Search pipeline, strategy, or selection"
                    value={index.search}
                    onValueChange={index.setSearch}
                  />
                  <Button type="submit" text="Search" />
                </form>
                {visible.length === 0 ? (
                  <p className="console-empty">
                    {index.query.search === undefined
                      ? "No export pipelines are configured."
                      : `No pipeline matches "${index.query.search}".`}
                  </p>
                ) : (
                  <HTMLTable className="console-exports__table console-index-table" interactive striped>
                    <thead>
                      <tr>
                        <th scope="col">Pipeline</th>
                        <th scope="col">Status</th>
                        <th scope="col">Build strategy</th>
                        <th scope="col">Selections</th>
                        <th scope="col">Guarantees</th>
                      </tr>
                    </thead>
                    <tbody>
                      {visible.map((pipeline) => (
                        <tr
                          key={pipeline.id}
                          data-testid={`pipeline-${pipeline.id}`}
                          onClick={() => setSelected(pipeline.id === selected ? undefined : pipeline.id)}
                          aria-selected={pipeline.id === selected}
                        >
                          <td>{pipeline.displayName === "" ? pipeline.id : pipeline.displayName}</td>
                          <td>
                            <Tag intent={pipelineStatusIntent(pipelineStatus(pipeline))} minimal round>
                              {pipelineStatus(pipeline).toLowerCase()}
                            </Tag>
                          </td>
                          <td>
                            <Tag minimal round>
                              {pipeline.buildStrategy}
                            </Tag>
                          </td>
                          <td>{pipeline.selections.length}</td>
                          <td>
                            {pipelineGuarantees(pipeline).length === 0 ? (
                              "—"
                            ) : (
                              pipelineGuarantees(pipeline).map((gate) => (
                                <Tag key={gate} intent="success" minimal round>
                                  {gate}
                                </Tag>
                              ))
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </HTMLTable>
                )}
                {chosen === undefined ? null : <PipelineDetail pipeline={chosen} />}
              </SectionCard>
            </Section>
          </div>
        );
      }}
    </QueryState>
  );
}

function pipelineMatches(pipeline: ExportPipeline, term: string): boolean {
  const haystacks = [
    pipeline.id,
    pipeline.displayName,
    pipeline.buildStrategy,
    pipelineStatus(pipeline),
    ...pipeline.selections.flatMap((selection) => [selection.type, ...selection.ids]),
    ...(pipeline.resourceTypes ?? []),
  ];
  return haystacks.some((value) => value.toLowerCase().includes(term));
}

function PipelineDetail({ pipeline }: { pipeline: ExportPipeline }) {
  const guarantees = pipelineGuarantees(pipeline);
  return (
    <section className="console-exports__detail" aria-labelledby="pipeline-detail-title" data-testid="pipeline-detail">
      <h2 id="pipeline-detail-title">{pipeline.displayName === "" ? pipeline.id : pipeline.displayName}</h2>
      <section aria-labelledby="pipeline-selections-title" data-testid="pipeline-selections">
        <h3 id="pipeline-selections-title">Selections</h3>
        {pipeline.selections.length === 0 ? (
          <p className="console-empty">This pipeline selects nothing, so it builds an empty Bundle.</p>
        ) : (
          <ul className="console-exports__selections">
            {pipeline.selections.map((selection, position) => (
              <li key={`${selection.type}-${position}`}>
                <Tag minimal round>
                  {selection.type}
                </Tag>{" "}
                {selection.ids.length === 0 ? "all" : selection.ids.join(", ")}
                {selection.releaseChannels !== undefined && selection.releaseChannels.length > 0
                  ? ` (channels: ${selection.releaseChannels.join(", ")})`
                  : ""}
              </li>
            ))}
          </ul>
        )}
      </section>
      <dl className="console-exports__meta">
        <div>
          <dt>Build strategy</dt>
          <dd>
            {pipeline.buildStrategy}
            {pipeline.buildStrategy === "INCREMENTAL" && pipeline.checkpointFrequency !== undefined
              ? ` (checkpoint every ${pipeline.checkpointFrequency})`
              : ""}
          </dd>
        </div>
        <div>
          <dt>Guarantees</dt>
          <dd>{guarantees.length === 0 ? "none enforced" : guarantees.join(", ")}</dd>
        </div>
        {pipeline.resourceTypes !== undefined && pipeline.resourceTypes.length > 0 ? (
          <div>
            <dt>Resource types</dt>
            <dd>{pipeline.resourceTypes.join(", ")}</dd>
          </div>
        ) : null}
      </dl>
    </section>
  );
}
