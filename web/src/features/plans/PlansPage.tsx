import { Button, H2, HTMLTable, InputGroup, Section, SectionCard } from "@blueprintjs/core";
import { Link } from "react-router-dom";

import { usePlans } from "../../api/resources.js";
import { EmptyState } from "../../components/EmptyState.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import { IndexPagination } from "../../components/IndexPagination.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { PlanStatusTag } from "../../components/PlanStatusTag.js";
import { useIndexQuery } from "../useIndexQuery.js";
import "./plans.css";

export function PlansPage() {
  const index = useIndexQuery();
  const { data, error, isPending, refetch } = usePlans(index.query);
  const querySubject = index.query.search === undefined ? "Plans" : `Plans matching "${index.query.search}"`;
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject={querySubject}
      onRetry={() => void refetch()}
    >
      {(page) => (
          <div className="console-plans" data-testid="plans-page">
            <ObjectHeader icon="gantt-chart" kind="Execution history" title="Plans" />
            <Section className="console-index" compact icon="gantt-chart" title="Visible Plans" titleRenderer={H2}>
              <SectionCard padded={false}>
                <form className="console-index-controls" role="search" onSubmit={index.applySearch}>
                  <InputGroup
                    leftIcon="search"
                    aria-label="Search Plans"
                    placeholder="Search Plan, Environment, Product, or Installation"
                    value={index.search}
                    onValueChange={index.setSearch}
                  />
                  <Button type="submit" text="Search" />
                </form>
                {page.plans.length === 0 ? (
                  <EmptyState
                    icon="gantt-chart"
                    title={
                      index.query.search === undefined
                        ? "No Plans have been created."
                        : `No Plan matches "${index.query.search}".`
                    }
                  />
                ) : (
                  <>
            <HTMLTable className="console-plans__table console-index-table" interactive striped>
              <thead>
                <tr>
                  <th scope="col">Plan</th>
                  <th scope="col">Environment</th>
                  <th scope="col">Status</th>
                  <th scope="col">Steps</th>
                  <th scope="col">Approvals</th>
                  <th scope="col">Created</th>
                </tr>
              </thead>
              <tbody>
                {page.plans.map((plan) => (
                  <tr key={plan.id}>
                    <td>
                      <Link to={`/plans/${encodeURIComponent(plan.id)}`}>{plan.id}</Link>
                    </td>
                    <td>{plan.environment}</td>
                    <td>
                      <PlanStatusTag value={plan.status} />
                    </td>
                    <td>{plan.stepCount}</td>
                    <td>
                      {plan.approvalCount} of {plan.requiredApprovals}
                    </td>
                    <td>
                      <Timestamp value={plan.createdAt} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
                    <div className="console-index-mobile-records" data-testid="plan-mobile-records">
                      {page.plans.map((plan) => (
                        <dl className="console-index-mobile-record" key={plan.id}>
                          <div><dt>Plan</dt><dd><Link to={`/plans/${encodeURIComponent(plan.id)}`}>{plan.id}</Link></dd></div>
                          <div><dt>Environment</dt><dd>{plan.environment}</dd></div>
                          <div><dt>Status</dt><dd>{plan.status}</dd></div>
                          <div><dt>Steps</dt><dd>{plan.stepCount}</dd></div>
                          <div><dt>Approvals</dt><dd>{plan.approvalCount} of {plan.requiredApprovals}</dd></div>
                        </dl>
                      ))}
                    </div>
                  </>
                )}
                <IndexPagination page={page} total={page.total} testId="plans-pagination" onMove={index.moveToCursor} />
              </SectionCard>
            </Section>
          </div>
      )}
    </QueryState>
  );
}
