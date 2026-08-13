import { Button, H2, HTMLTable, InputGroup, Section, SectionCard, Tag } from "@blueprintjs/core";
import { Link } from "react-router-dom";

import { useEnvironments } from "../../api/resources.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import { IndexPagination } from "../../components/IndexPagination.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { useIndexQuery } from "../useIndexQuery.js";
import "./environments.css";

/** Environment index -- P0: "Which Environments are connected and healthy?"
 *
 * The connection setting is shown as a tag on the row, which is where upstream
 * puts it too: capture 065 is a tag in the Environment header showing the
 * connection setting. An Environment that has never reported is called out
 * rather than shown as an empty row, because those are different conditions:
 * one has no Agent, the other has an Agent with nothing to say. */
export function EnvironmentsPage() {
  const index = useIndexQuery();
  const { data, error, isPending, refetch } = useEnvironments(index.query);
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Environments"
      onRetry={() => void refetch()}
    >
      {(page) => (
        <div data-testid="environments-page">
            <ObjectHeader icon="cloud" kind="Resource collection" title="Environments" />
            <Section className="console-index" compact icon="cloud" title="Visible Environments" titleRenderer={H2}>
              <SectionCard padded={false}>
                <form className="console-index-controls" role="search" onSubmit={index.applySearch}>
                  <InputGroup
                    leftIcon="search"
                    aria-label="Search Environments"
                    placeholder="Search Environment ID"
                    value={index.search}
                    onValueChange={index.setSearch}
                  />
                  <Button type="submit" text="Search" />
                </form>
                {page.environments.length === 0 ? (
                  <p className="console-empty">
                    {index.query.search === undefined
                      ? "No Environments are known to this Hub."
                      : `No Environment matches "${index.query.search}".`}
                  </p>
                ) : (
                  <>
            <HTMLTable className="console-environments__table console-index-table" interactive striped>
              <thead>
                <tr>
                  <th scope="col">Environment</th>
                  <th scope="col">Connection</th>
                  <th scope="col">Entities reported</th>
                  <th scope="col">Last report</th>
                </tr>
              </thead>
              <tbody>
                {page.environments.map((record) => (
                  <tr key={record.environment.id}>
                    <td>
                      <Link to={`/environments/${encodeURIComponent(record.environment.id)}`}>
                        {record.environment.id}
                      </Link>
                    </td>
                    <td>
                      {record.environment.connectionSetting === undefined ? (
                        <Tag minimal round>
                          unset
                        </Tag>
                      ) : (
                        <Tag round minimal>
                          {record.environment.connectionSetting}
                        </Tag>
                      )}
                    </td>
                    <td>
                      {record.reported === undefined ? (
                        <Tag intent="warning" minimal round>
                          never reported
                        </Tag>
                      ) : (
                        record.reported.entityCount
                      )}
                    </td>
                    <td>
                      <Timestamp value={record.reported?.observedAt} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
                  <div className="console-index-mobile-records" data-testid="environment-mobile-records">
                    {page.environments.map((record) => (
                      <dl className="console-index-mobile-record" key={record.environment.id}>
                        <div><dt>Environment</dt><dd><Link to={`/environments/${encodeURIComponent(record.environment.id)}`}>{record.environment.id}</Link></dd></div>
                        <div><dt>Connection</dt><dd>{record.environment.connectionSetting ?? "unset"}</dd></div>
                        <div><dt>Entities reported</dt><dd>{record.reported?.entityCount ?? "never reported"}</dd></div>
                        <div><dt>Last report</dt><dd><Timestamp value={record.reported?.observedAt} /></dd></div>
                      </dl>
                    ))}
                  </div>
                  </>
                )}
                <IndexPagination
                  page={page}
                  total={page.total}
                  testId="environments-pagination"
                  onMove={index.moveToCursor}
                />
              </SectionCard>
            </Section>
          </div>
      )}
    </QueryState>
  );
}
