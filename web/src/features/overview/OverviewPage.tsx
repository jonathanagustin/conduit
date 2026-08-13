import { Callout, Section, SectionCard, Spinner, Tag } from "@blueprintjs/core";
import { Link } from "react-router-dom";
import type { HTMLAttributes } from "react";

import { useOverview, type OverviewPanel } from "../../api/overview.js";
import { countOf } from "../../components/Timestamp.js";
import { AccessibleNonIdealState, ObjectHeader } from "../../components/index.js";
import "./overview.css";

/** Overview -- P0: "What needs attention now?"
 *
 * This screen answers one question, so it shows only things that need a person:
 * a healthy fleet, an empty approval queue and no blocked promotions produce a
 * screen that says there is nothing to do, rather than four empty panels. It
 * The Hub supplies one bounded, authorization-filtered snapshot. Each source
 * carries independent availability so a partial read can never look clear. */
export function OverviewPage() {
  const overview = useOverview();

  if (overview.isPending) {
    return (
      <AccessibleNonIdealState
        headingLevel={1}
        icon={<Spinner />}
        title="Reading the control plane"
      />
    );
  }

  const projection = overview.data;
  const awaiting = panelOrUnavailable(projection?.awaitingApprovals);
  const failedPlans = panelOrUnavailable(projection?.failedPlans);
  const runningPlans = panelOrUnavailable(projection?.runningPlans);
  const unhealthy = panelOrUnavailable(projection?.unhealthyEntities);
  const stale = panelOrUnavailable(projection?.staleReports);
  const blocked = panelOrUnavailable(projection?.blockedPromotions);
  const unreadable = [
    awaiting.status === "UNAVAILABLE" ? "Change Requests" : null,
    failedPlans.status === "UNAVAILABLE" || runningPlans.status === "UNAVAILABLE" ? "Plans" : null,
    unhealthy.status === "UNAVAILABLE" || stale.status === "UNAVAILABLE" ? "the fleet" : null,
    blocked.status === "UNAVAILABLE" ? "promotions" : null,
  ].filter((subject): subject is string => subject !== null);

  const nothing =
    unhealthy.count === 0 &&
    stale.count === 0 &&
    awaiting.count === 0 &&
    failedPlans.count === 0 &&
    runningPlans.count === 0 &&
    blocked.count === 0;

  return (
    <div className="console-overview" data-testid="overview-page">
      <ObjectHeader icon="dashboard" kind="Control plane" title="Overview" />
      {unreadable.length > 0 ? (
        <Callout intent="warning" icon="warning-sign" title="This page is incomplete">
          {unreadable.join(", ")} could not be read, so anything below is a partial answer.
        </Callout>
      ) : null}

      {nothing && unreadable.length === 0 ? (
        <AccessibleNonIdealState
          icon="tick-circle"
          title="Nothing needs attention"
          description="Every Entity is healthy and reporting, no Change Request is awaiting approval, no Plan is running or failed, and no promotion is blocked."
        />
      ) : null}

      <div className="console-overview__grid">
        <Panel
          title="Awaiting approval"
          count={awaiting.count}
          to="/change-requests"
          unknown={awaiting.status === "UNAVAILABLE"}
        >
          {awaiting.items.map((request) => (
            <li key={request.id}>
              <Link to={`/change-requests/${encodeURIComponent(request.id)}`}>{request.title}</Link>{" "}
              <Tag minimal round>
                {request.environment}
              </Tag>
            </li>
          ))}
        </Panel>

        <Panel
          title="Failed Plans"
          count={failedPlans.count}
          to="/plans"
          intent="danger"
          unknown={failedPlans.status === "UNAVAILABLE"}
        >
          {failedPlans.items.map((plan) => (
            <li key={plan.id}>
              <Link to={`/plans/${encodeURIComponent(plan.id)}`}>{plan.id}</Link>{" "}
              <Tag minimal round>
                {plan.environment}
              </Tag>
            </li>
          ))}
        </Panel>

        <Panel
          title="Running Plans"
          count={runningPlans.count}
          to="/plans"
          unknown={runningPlans.status === "UNAVAILABLE"}
        >
          {runningPlans.items.map((plan) => (
            <li key={plan.id}>
              <Link to={`/plans/${encodeURIComponent(plan.id)}`}>{plan.id}</Link>
            </li>
          ))}
        </Panel>

        <Panel
          title="Unhealthy Entities"
          count={unhealthy.count}
          to="/fleet"
          intent="danger"
          unknown={unhealthy.status === "UNAVAILABLE"}
        >
          {unhealthy.items.map((item) => (
            <li key={`${item.environment} ${item.installationId}`}>
              {item.installationId}{" "}
              <Tag minimal round>
                {item.environment}
              </Tag>{" "}
              <Tag intent="danger" round>
                {item.health}
              </Tag>
            </li>
          ))}
        </Panel>

        <Panel
          title="Stale reports"
          count={stale.count}
          to="/fleet"
          intent="warning"
          unknown={stale.status === "UNAVAILABLE"}
        >
          {stale.count > 0 ? (
            <li>{countOf(stale.count, "installation")} not reporting recently.</li>
          ) : null}
        </Panel>

        <Panel
          title="Blocked promotions"
          count={blocked.count}
          to="/promotions"
          intent="warning"
          unknown={blocked.status === "UNAVAILABLE"}
        >
          {blocked.items.map((promotion) => (
            <li key={promotion.id}>
              {promotion.product} {promotion.version}{" "}
              <Tag minimal round>
                {promotion.fromChannel} &rarr; {promotion.toChannel}
              </Tag>
            </li>
          ))}
        </Panel>
      </div>
    </div>
  );
}

function panelOrUnavailable<T>(panel: OverviewPanel<T> | undefined): OverviewPanel<T> {
  return panel ?? { status: "UNAVAILABLE", count: 0, items: [] };
}

function SectionHeading(props: HTMLAttributes<HTMLElement>) {
  return <h2 {...props} />;
}

function Panel({
  title,
  count,
  to,
  intent,
  unknown = false,
  children,
}: {
  title: string;
  count: number;
  to: string;
  intent?: "danger" | "warning";
  unknown?: boolean;
  children: React.ReactNode;
}) {
  // Blueprint's Section is the white, thin-bordered panel the plan's recurring
  // composition asks for. An earlier version of this file built one out of Card,
  // which the visual system explicitly forbids: "Do not introduce oversized
  // dashboard cards".
  return (
    <Section
      className="console-overview__panel"
      compact
      titleRenderer={SectionHeading}
      title={<Link to={to}>{title}</Link>}
      rightElement={
        <Tag
          intent={unknown || count === 0 ? "none" : (intent ?? "primary")}
          round
          minimal={unknown || count === 0}
        >
          {unknown ? "Unknown" : count}
        </Tag>
      }
    >
      <SectionCard padded>
        {/* An empty panel here is good news, and should read as an answer
            rather than as a screen that failed to load. */}
        {unknown ? (
          <span className="console-overview__clear">Could not be read</span>
        ) : count === 0 ? (
          <span className="console-overview__clear">Clear</span>
        ) : (
          <ul>{children}</ul>
        )}
      </SectionCard>
    </Section>
  );
}
