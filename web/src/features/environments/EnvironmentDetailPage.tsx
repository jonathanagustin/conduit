import { useState } from "react";
import { Callout, H2, HTMLTable, Section, SectionCard, Tab, Tabs, Tag } from "@blueprintjs/core";
import { NavLink, useParams } from "react-router-dom";

import { useEnvironment, useEnvironmentCommands } from "../../api/catalog.js";
import type { EnvironmentCatalogRecord } from "../../api/resources.js";
import { usePlans } from "../../api/resources.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import { HealthTag, LifecycleTag } from "../../components/statusTags.js";
import { PlanStatusTag } from "../../components/PlanStatusTag.js";
import "./environments.css";

/** Environment detail -- P0: "What is happening in this Environment?"
 *
 * The tab set is the upstream one, evidenced by captures 206 (Overview), 200
 * (Changes) and 157 (Commands) in docs/ui/upstream-screen-evidence.tsv. Only
 * the tabs backed by an endpoint are present: an empty tab that exists because
 * upstream has one would be a claim this Hub cannot honour. Plans is here in
 * place of upstream's Changes tab and is separately evidenced by capture 058,
 * an Environment-level Plans view. */
export function EnvironmentDetailPage() {
  const { environmentId = "" } = useParams();
  const [tab, setTab] = useState<string>("overview");
  const environment = useEnvironment(environmentId);

  return (
    <QueryState
      isPending={environment.isPending}
      error={environment.error}
      data={environment.data}
      subject={`Environment ${environmentId}`}
      onRetry={() => void environment.refetch()}
    >
      {(record) => (
        <div className="console-environment" data-testid="environment-detail">
          <ObjectHeader
            icon="cloud"
            kind="Environment"
            title={record.environment.id}
            {...(record.reported?.agentId === undefined
              ? {}
              : { subtitle: `reporting via ${record.reported.agentId}` })}
            tags={
              record.environment.connectionSetting === undefined ? null : (
                <Tag round minimal>
                  {record.environment.connectionSetting}
                </Tag>
              )
            }
          />

          <Tabs
            id="environment-tabs"
            selectedTabId={tab}
            onChange={(next) => setTab(String(next))}
            renderActiveTabPanelOnly
          >
            <Tab
              id="overview"
              title="Overview"
              panel={
                <>
                  <OverviewSummary record={record} />
                  <EntitiesPanel record={record} />
                </>
              }
            />
            <Tab
              id="plans"
              title="Plans"
              panel={<PlansPanel environmentId={environmentId} />}
            />
            <Tab
              id="commands"
              title="Commands"
              panel={<CommandsPanel environmentId={environmentId} />}
            />
          </Tabs>
        </div>
      )}
    </QueryState>
  );
}

/** The health of an Environment at a glance, above its Entities.
 *
 * The plan's wireframe 2 leads with health (38/40), the desired and reported
 * generation, and config drift -- and V1 capture 206 is "the Environment
 * Overview tab ... Each section is highlighted and given a number", so upstream
 * treats Overview as sections rather than as one table. Counting here rather
 * than asking the Hub is deliberate: the numbers come from the same report the
 * table below renders, so they cannot disagree with it. */
function OverviewSummary({ record }: { record: EnvironmentCatalogRecord }) {
  const entities = record.reported?.entities ?? [];
  const healthy = entities.filter((entity) => entity.health === "HEALTHY").length;
  const drifted = entities.filter((entity) => entity.configStatus === "DRIFTED").length;
  const reportedGeneration = record.reported?.environment;
  return (
    <div className="console-environment__summary" data-testid="environment-overview-summary">
      <Section compact title="Health" titleRenderer={H2}>
        <SectionCard padded>
          {entities.length === 0 ? (
            <span className="console-empty">Nothing reported.</span>
          ) : (
            <>
              <strong>
                {healthy} of {entities.length}
              </strong>{" "}
              Entities healthy
              {drifted > 0 ? (
                <>
                  {" "}
                  &middot;{" "}
                  <Tag intent="warning" minimal round>
                    {drifted} drifted
                  </Tag>
                </>
              ) : null}
            </>
          )}
        </SectionCard>
      </Section>
      <Section compact title="Generation" titleRenderer={H2}>
        <SectionCard padded>
          {/* Desired and reported are shown separately because them disagreeing
              is what tells an operator the Agent has not caught up. */}
          desired <strong>{record.environment.generation ?? "—"}</strong>
          {reportedGeneration !== undefined ? (
            <>
              {" "}
              &middot; last report from <strong>{record.reported?.agentId ?? "an Agent"}</strong>{" "}
              <Timestamp value={record.reported?.observedAt} />
            </>
          ) : null}
        </SectionCard>
      </Section>
    </div>
  );
}

function EntitiesPanel({ record }: { record: EnvironmentCatalogRecord }) {
  const entities = record.reported?.entities ?? [];
  if (record.reported === undefined) {
    return (
      <Callout intent="warning" icon="offline" title="This Environment has never reported">
        No Agent has submitted state for it, so there is nothing observed to show. What the Hub
        intends for it is in its Plans.
      </Callout>
    );
  }
  if (entities.length === 0) {
    return <p className="console-empty">The last report contained no Entities.</p>;
  }
  return (
    <>
      <HTMLTable className="console-environments__table console-environment__entity-table" striped>
        <thead>
          <tr>
            <th scope="col">Entity</th>
            <th scope="col">Product</th>
            <th scope="col">Version</th>
            <th scope="col">Health</th>
            <th scope="col">Lifecycle</th>
          </tr>
        </thead>
        <tbody>
          {entities.map((entity) => (
            <tr key={entity.installationId} data-testid={`environment-entity-row-${entity.installationId}`}>
              <td>{entity.installationId}</td>
              <td>{entity.product}</td>
              <td>{entity.version}</td>
              <td>{entity.health === undefined ? "—" : <HealthTag value={entity.health} />}</td>
              <td>
                {entity.lifecycle === undefined ? "—" : <LifecycleTag value={entity.lifecycle} />}
              </td>
            </tr>
          ))}
        </tbody>
      </HTMLTable>
      <div className="console-environment__mobile-records" aria-label="Reported Entities">
        {entities.map((entity) => (
          <dl
            className="console-environment__mobile-record"
            key={entity.installationId}
            data-testid={`environment-entity-card-${entity.installationId}`}
          >
            <div>
              <dt>Entity</dt>
              <dd>{entity.installationId}</dd>
            </div>
            <div>
              <dt>Product</dt>
              <dd>{entity.product}</dd>
            </div>
            <div>
              <dt>Version</dt>
              <dd>{entity.version}</dd>
            </div>
            <div>
              <dt>Health</dt>
              <dd>{entity.health ?? "—"}</dd>
            </div>
            <div>
              <dt>Lifecycle</dt>
              <dd>{entity.lifecycle ?? "—"}</dd>
            </div>
          </dl>
        ))}
      </div>
    </>
  );
}

function PlansPanel({ environmentId }: { environmentId: string }) {
  const { data, error, isPending, refetch } = usePlans({}, environmentId);
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Plans"
      onRetry={() => void refetch()}
    >
      {(page) =>
        page.plans.length === 0 ? (
          <p className="console-empty">This Environment has no Plans.</p>
        ) : (
          <>
            <HTMLTable className="console-environments__table console-environment__responsive-table" striped>
              <thead>
                <tr>
                  <th scope="col">Plan</th>
                  <th scope="col">Status</th>
                  <th scope="col">Steps</th>
                </tr>
              </thead>
              <tbody>
                {page.plans.map((plan) => (
                  <tr key={plan.id} data-testid={`environment-plan-row-${plan.id}`}>
                    <td>
                      <NavLink to={`/plans/${encodeURIComponent(plan.id)}`}>{plan.id}</NavLink>
                    </td>
                    <td>
                      <PlanStatusTag value={plan.status} />
                    </td>
                    <td>{plan.stepCount}</td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
            <div className="console-environment__mobile-records" aria-label="Plans">
              {page.plans.map((plan) => (
                <dl
                  className="console-environment__mobile-record"
                  key={plan.id}
                  data-testid={`environment-plan-card-${plan.id}`}
                >
                  <div>
                    <dt>Plan</dt>
                    <dd>
                      <NavLink to={`/plans/${encodeURIComponent(plan.id)}`}>{plan.id}</NavLink>
                    </dd>
                  </div>
                  <div>
                    <dt>Status</dt>
                    <dd>{plan.status}</dd>
                  </div>
                  <div>
                    <dt>Steps</dt>
                    <dd>{plan.stepCount}</dd>
                  </div>
                </dl>
              ))}
            </div>
          </>
        )
      }
    </QueryState>
  );
}

function CommandsPanel({ environmentId }: { environmentId: string }) {
  const { data, error, isPending, refetch } = useEnvironmentCommands(environmentId);
  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject="Commands"
      onRetry={() => void refetch()}
    >
      {(commands) =>
        commands.length === 0 ? (
          <p className="console-empty">No commands have been executed in this Environment.</p>
        ) : (
          <>
            <HTMLTable className="console-environments__table console-environment__responsive-table" striped>
              <thead>
                <tr>
                  <th scope="col">Command</th>
                  <th scope="col">Entity</th>
                  <th scope="col">Issued by</th>
                  <th scope="col">Rationale</th>
                  <th scope="col">Plan status</th>
                  <th scope="col">Issued</th>
                </tr>
              </thead>
              <tbody>
                {/* The command is nested inside the Plan that carries it, and the
                  status on the row is the PLAN's -- a command has no status of
                  its own. Labelling the column "Plan status" rather than
                  "Status" is the honest rendering of that. */}
                {commands.map((record) => (
                  <tr key={record.id} data-testid={`environment-command-row-${record.id}`}>
                    <td>{record.command?.type ?? "—"}</td>
                    <td>{record.command?.installationId ?? "—"}</td>
                    <td>{record.command?.issuedBy ?? "—"}</td>
                    <td>{record.command?.rationale ?? "—"}</td>
                    <td>
                      <PlanStatusTag value={record.status} />
                    </td>
                    <td>
                      <Timestamp value={record.command?.issuedAt} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
            <div className="console-environment__mobile-records" aria-label="Commands">
              {commands.map((record) => (
                <dl
                  className="console-environment__mobile-record"
                  key={record.id}
                  data-testid={`environment-command-card-${record.id}`}
                >
                  <div>
                    <dt>Command</dt>
                    <dd>{record.command?.type ?? "—"}</dd>
                  </div>
                  <div>
                    <dt>Entity</dt>
                    <dd>{record.command?.installationId ?? "—"}</dd>
                  </div>
                  <div>
                    <dt>Issued by</dt>
                    <dd>{record.command?.issuedBy ?? "—"}</dd>
                  </div>
                  <div>
                    <dt>Rationale</dt>
                    <dd>{record.command?.rationale ?? "—"}</dd>
                  </div>
                  <div>
                    <dt>Plan status</dt>
                    <dd>{record.status}</dd>
                  </div>
                  <div>
                    <dt>Issued</dt>
                    <dd>
                      <Timestamp value={record.command?.issuedAt} />
                    </dd>
                  </div>
                </dl>
              ))}
            </div>
          </>
        )
      }
    </QueryState>
  );
}
