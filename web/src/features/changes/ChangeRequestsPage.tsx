import { HTMLTable, Section, SectionCard, Tag} from "@blueprintjs/core";
import { Link } from "react-router-dom";

import { useChangeRequests } from "../../api/resources.js";
import { QueryState } from "../../components/QueryState.js";
import { ChangeStatusTag } from "./ChangeRequestDetailPage.js";
import { orderInbox, outstandingRequirements } from "./order.js";
import "./changes.css";

/** The inbox -- P0: "What am I being asked to approve?"
 *
 * Requests awaiting approval sort first because they are the only ones that
 * need an operator; everything else is history and is ordered by recency. */
export function ChangeRequestsPage() {
  const { data, error, isPending, refetch } = useChangeRequests();
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Change Requests"
      onRetry={() => void refetch()}
    >
      {(requests) => {
        const ordered = orderInbox(requests);
        return ordered.length === 0 ? (
          <p className="console-empty">There are no Change Requests.</p>
        ) : (
          <div data-testid="change-requests-page">
            <Section className="console-index" compact icon="inbox" title="Change Requests">
              <SectionCard padded={false}>
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
              </SectionCard>
            </Section>
          </div>
        );
      }}
    </QueryState>
  );
}
