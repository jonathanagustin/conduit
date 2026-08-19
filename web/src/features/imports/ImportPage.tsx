import { useState } from "react";
import { Button, H2, HTMLTable, InputGroup, Section, SectionCard, Tag } from "@blueprintjs/core";
import { Link } from "react-router-dom";

import { statusIntent, useBundleImports, type BundleImport } from "../../api/imports.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import { useIndexQuery } from "../useIndexQuery.js";
import "./imports.css";

/** Import -- the record of every offline Bundle imported into an Environment.
 * GET /v1/bundle-imports returns the whole set as one named array, so the
 * search filters locally. Selecting an import reveals its per-stage tasks from
 * the row already loaded, so "where did this import get to, or where did it
 * fail" is answered without a second request. */
export function ImportPage() {
  const index = useIndexQuery();
  const { data, error, isPending, refetch } = useBundleImports();
  const [selected, setSelected] = useState<string | undefined>(undefined);
  const term = (index.query.search ?? "").toLowerCase();
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Bundle imports"
      onRetry={() => void refetch()}
    >
      {(imports) => {
        const visible = term === "" ? imports : imports.filter((record) => importMatches(record, term));
        const chosen = visible.find((record) => record.id === selected);
        return (
          <div data-testid="import-page">
            <ObjectHeader icon="import" kind="Resource collection" title="Import" />
            <Section className="console-index" compact icon="import" title="Bundle imports" titleRenderer={H2}>
              <SectionCard padded={false}>
                <form className="console-index-controls" role="search" onSubmit={index.applySearch}>
                  <InputGroup
                    leftIcon="search"
                    aria-label="Search bundle imports"
                    placeholder="Search Bundle, Environment, status, or requester"
                    value={index.search}
                    onValueChange={index.setSearch}
                  />
                  <Button type="submit" text="Search" />
                </form>
                {visible.length === 0 ? (
                  <p className="console-empty">
                    {index.query.search === undefined
                      ? "No Bundles have been imported."
                      : `No import matches "${index.query.search}".`}
                  </p>
                ) : (
                  <HTMLTable className="console-imports__table console-index-table" interactive striped>
                    <thead>
                      <tr>
                        <th scope="col">Bundle</th>
                        <th scope="col">Environment</th>
                        <th scope="col">Status</th>
                        <th scope="col">Requested by</th>
                        <th scope="col">Requested</th>
                      </tr>
                    </thead>
                    <tbody>
                      {visible.map((record) => (
                        <tr
                          key={record.id}
                          data-testid={`import-${record.id}`}
                          onClick={() => setSelected(record.id === selected ? undefined : record.id)}
                          aria-selected={record.id === selected}
                        >
                          <td className="console-imports__bundle">{record.bundleId}</td>
                          <td>
                            <Link
                              to={`/environments/${encodeURIComponent(record.environment)}`}
                              onClick={(event) => event.stopPropagation()}
                            >
                              {record.environment}
                            </Link>
                          </td>
                          <td>
                            <Tag intent={statusIntent(record.status)} round>
                              {record.status}
                            </Tag>
                          </td>
                          <td>{record.requestedBy}</td>
                          <td>
                            <Timestamp value={record.requestedAt} />
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </HTMLTable>
                )}
                {chosen === undefined ? null : <ImportDetail record={chosen} />}
              </SectionCard>
            </Section>
          </div>
        );
      }}
    </QueryState>
  );
}

function importMatches(record: BundleImport, term: string): boolean {
  const haystacks = [
    record.bundleId,
    record.manifestDigest,
    record.environment,
    record.status,
    record.requestedBy,
    ...record.tasks.map((task) => task.stage),
  ];
  return haystacks.some((value) => value.toLowerCase().includes(term));
}

function ImportDetail({ record }: { record: BundleImport }) {
  const tasks = [...record.tasks].sort((left, right) => left.sequence - right.sequence);
  return (
    // A real Blueprint Section, not a bare <section>: the panel's white surface,
    // 1px hairline and 4px corner now come from the primitive
    // (--bp-surface-border-radius), matching the products detail panels, instead
    // of a border/radius rule on raw HTML. It stays inline and nonmodal -- the
    // selected row's context is preserved, so this is a primitive swap, not a
    // dialog.
    <Section
      className="console-imports__detail"
      compact
      title={`Import of ${record.bundleId}`}
      titleRenderer={H2}
      data-testid="import-detail"
    >
      <SectionCard>
      <dl className="console-imports__meta">
        <div>
          <dt>Manifest digest</dt>
          <dd className="console-imports__digest">{record.manifestDigest}</dd>
        </div>
        {record.changeRequestId !== undefined && record.changeRequestId !== "" ? (
          <div>
            <dt>Change request</dt>
            <dd>
              <Link to={`/change-requests/${encodeURIComponent(record.changeRequestId)}`}>
                {record.changeRequestId}
              </Link>
            </dd>
          </div>
        ) : null}
        {record.importedAt !== undefined && record.importedAt !== "" ? (
          <div>
            <dt>Imported</dt>
            <dd>
              <Timestamp value={record.importedAt} />
            </dd>
          </div>
        ) : null}
      </dl>
      {record.error !== undefined && record.error !== "" ? (
        <p className="console-imports__error" role="alert">
          {record.error}
        </p>
      ) : null}
      <h3>Tasks</h3>
      {tasks.length === 0 ? (
        <p className="console-empty">This import recorded no task stages.</p>
      ) : (
        <ol className="console-imports__tasks">
          {tasks.map((task) => (
            <li key={task.sequence} data-testid={`import-task-${task.sequence}`}>
              <Tag intent={statusIntent(task.status)} minimal round>
                {task.status}
              </Tag>{" "}
              <span className="console-imports__stage">{task.stage}</span>
              {task.resources !== undefined && task.resources.length > 0 ? (
                <span className="console-imports__resources"> — {task.resources.join(", ")}</span>
              ) : null}
              {task.failure !== undefined && task.failure !== "" ? (
                <span className="console-imports__failure"> — {task.failure}</span>
              ) : null}
            </li>
          ))}
        </ol>
      )}
      </SectionCard>
    </Section>
  );
}
