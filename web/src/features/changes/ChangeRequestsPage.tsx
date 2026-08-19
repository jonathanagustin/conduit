import { Button, H2, HTMLTable, InputGroup, Section, SectionCard, Tag } from "@blueprintjs/core";
import { Link } from "react-router-dom";

import { useChangeRequests } from "../../api/resources.js";
import { EmptyState } from "../../components/EmptyState.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { ChangeStatusTag } from "./ChangeRequestDetailPage.js";
import { IndexPagination } from "../../components/IndexPagination.js";
import { useIndexQuery } from "../useIndexQuery.js";
import { orderInbox, outstandingRequirements } from "./order.js";
import "./changes.css";

/** The inbox -- P0: "What am I being asked to approve?"
 *
 * Requests awaiting approval sort first because they are the only ones that
 * need an operator; everything else is history and is ordered by recency. */
export function ChangeRequestsPage() {
  const index = useIndexQuery();
  const { data, error, isPending, refetch } = useChangeRequests(index.query);
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Change Requests"
      onRetry={() => void refetch()}
    >
      {(page) => {
        const ordered = orderInbox(page.changeRequests);
        return (
          <div data-testid="change-requests-page">
            {/* Mount the shared identity row and title the list Section with the
                shared H2 renderer, so this index's chrome matches every other
                index screen (environments, products, plans, exports, imports,
                risk, teams) instead of diverging. */}
            <ObjectHeader icon="inbox" kind="Resource collection" title="Change Requests" />
            <Section className="console-index" compact icon="inbox" title="Change Requests" titleRenderer={H2}>
              <SectionCard padded={false}>
                <form className="console-index-controls" role="search" onSubmit={index.applySearch}>
                  <InputGroup
                    leftIcon="search"
                    aria-label="Search Change Requests"
                    placeholder="Search request, title, Environment, or author"
                    value={index.search}
                    onValueChange={index.setSearch}
                  />
                  <Button type="submit" text="Search" />
                </form>
                {ordered.length === 0 ? (
                  <EmptyState
                    icon="git-pull"
                    title={
                      index.query.search === undefined
                        ? "There are no Change Requests."
                        : `No Change Request matches "${index.query.search}".`
                    }
                  />
                ) : (
                  <HTMLTable className="console-change__table" interactive striped>
                    <thead>
                      <tr>
                        <th scope="col">Request</th>
                        <th scope="col">Title</th>
                        <th scope="col">Environment</th>
                        <th scope="col">Author</th>
                        <th scope="col">Status</th>
                        <th scope="col">Outstanding</th>
                      </tr>
                    </thead>
                    <tbody>
                      {ordered.map((request) => {
                        const outstanding = outstandingRequirements(request);
                        return (
                          <tr key={request.id}>
                            <td>
                              <Link to={`/change-requests/${encodeURIComponent(request.id)}`}>
                                {request.id}
                              </Link>
                            </td>
                            <td>{request.title}</td>
                            <td>{request.environment}</td>
                            <td>{request.author.username}</td>
                            <td>
                              <ChangeStatusTag value={request.status} />
                            </td>
                            <td>
                              {outstanding === 0 ? (
                                <Tag minimal round>
                                  none
                                </Tag>
                              ) : (
                                <Tag intent="warning" round>
                                  {outstanding}
                                </Tag>
                              )}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </HTMLTable>
                )}
                <IndexPagination
                  page={page}
                  total={page.total}
                  testId="change-requests-pagination"
                  onMove={index.moveToCursor}
                />
              </SectionCard>
            </Section>
          </div>
        );
      }}
    </QueryState>
  );
}
