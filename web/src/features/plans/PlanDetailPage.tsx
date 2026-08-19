import { Callout, H4, HTMLTable, Pre, Section, SectionCard, Tag } from "@blueprintjs/core";
import { useParams } from "react-router-dom";

import { usePlan } from "../../api/resources.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import { Timestamp } from "../../components/Timestamp.js";
import { PlanStatusTag } from "../../components/PlanStatusTag.js";
import "./plans.css";

/** Plan detail -- P0: "What will or did execute?"
 *
 * The failure evidence here is modelled on the upstream Details panel: capture
 * 052 lists each Kubernetes resource that failed to become ready, and 053 shows
 * the Plan timeline beside the raw Agent output. Both are bounded and redacted
 * by the Hub before they arrive. */
export function PlanDetailPage() {
  const { planId = "" } = useParams();
  const { data, error, isPending, refetch } = usePlan(planId);

  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject={`Plan ${planId}`}
      onRetry={() => void refetch()}
    >
      {(plan) => (
        <div className="console-plan-detail" data-testid="plan-detail">
          <ObjectHeader
            icon="gantt-chart"
            kind="Plan"
            title={plan.id}
            subtitle={
              <>
                {plan.plan.environment} &middot; generation {plan.plan.generation} &middot; created{" "}
                <Timestamp value={plan.createdAt} />
              </>
            }
            tags={
              <>
                <PlanStatusTag value={plan.status} />
                <Tag minimal>
                  {(plan.approvals ?? []).length} of {plan.requiredApprovals} approvals
                </Tag>
              </>
            }
          />

          {plan.failure !== undefined && plan.failure !== "" ? (
            <Callout intent="danger" title="This Plan failed" icon="error">
              {plan.failure}
              {plan.failedInstallationId !== undefined && plan.failedInstallationId !== "" ? (
                <> The failure is attributed to {plan.failedInstallationId}.</>
              ) : null}
            </Callout>
          ) : null}

          {plan.cancellation !== undefined ? (
            <Callout intent="warning" title="This Plan was cancelled" icon="disable">
              {plan.cancellation.subject}: {plan.cancellation.reason}
            </Callout>
          ) : null}

          <H4>Steps</H4>
          {plan.plan.steps.length === 0 ? (
            <p className="console-empty">This Plan has no steps.</p>
          ) : (
            <HTMLTable className="console-plan-detail__table" striped>
              <thead>
                <tr>
                  <th scope="col">#</th>
                  <th scope="col">Action</th>
                  <th scope="col">Entity</th>
                  <th scope="col">Product</th>
                  <th scope="col">Version</th>
                  <th scope="col">Namespace</th>
                </tr>
              </thead>
              <tbody>
                {plan.plan.steps.map((step) => (
                  <tr key={`${step.position} ${step.installationId}`}>
                    <td>{step.position}</td>
                    <td>{step.action}</td>
                    <td>{step.installationId}</td>
                    <td>{step.product}</td>
                    <td>{step.version}</td>
                    <td>{step.namespace}</td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
          )}

          <H4>Task timeline</H4>
          {(plan.execution?.tasks ?? []).length === 0 ? (
            <p className="console-empty">
              This Plan has not executed, so there is no timeline to show.
            </p>
          ) : (
            <ol className="console-plan-detail__timeline">
              {(plan.execution?.tasks ?? []).map((task) => (
                <li
                  key={task.sequence}
                  data-status={task.status}
                  aria-label={`${task.phase} task`}
                  data-testid={`plan-task-${task.sequence}`}
                >
                  <div className="console-plan-detail__task">
                    <Tag minimal round>
                      {task.phase}
                    </Tag>
                    <Tag
                      intent={
                        task.status === "SUCCEEDED"
                          ? "success"
                          : task.status.includes("FAILED")
                            ? "danger"
                            : "none"
                      }
                      round
                    >
                      {task.status}
                    </Tag>
                    {task.installationId !== undefined && task.installationId !== "" ? (
                      <span>{task.installationId}</span>
                    ) : null}
                  </div>
                  {/* A failed task does not stop the ones behind it, so failure
                      text belongs on the task rather than on the Plan. */}
                  {task.failure !== undefined && task.failure !== "" ? (
                    <Callout intent="danger" compact>
                      {task.failure}
                    </Callout>
                  ) : null}
                  {(task.resources ?? []).length > 0 ? (
                    // A real Blueprint Section, not a styled <details>: the
                    // collapsible disclosure now comes from the primitive, so
                    // the panel's white surface, 1px hairline and 4px corner
                    // (--bp-surface-border-radius) are the primitive's, not a
                    // radius rule on a raw element. A failed task opens by
                    // default because its diagnostics are why the Plan stopped.
                    <Section
                      className="console-plan-detail__diagnostics"
                      compact
                      collapsible
                      collapseProps={{ defaultIsOpen: task.status.includes("FAILED") }}
                      title={`${(task.resources ?? []).length} ${
                        (task.resources ?? []).length === 1
                          ? "resource diagnostic"
                          : "resource diagnostics"
                      }`}
                    >
                      <SectionCard padded={false}>
                      <ul
                        aria-label={`Resource diagnostics for ${task.phase}`}
                        data-testid={`resource-diagnostics-${task.sequence}`}
                      >
                        {(task.resources ?? []).map((resource, index) => (
                          <li
                            key={`${resource.apiVersion ?? ""} ${resource.kind} ${resource.namespace ?? ""} ${resource.name} ${index}`}
                          >
                            <dl>
                              <div>
                                <dt>Resource</dt>
                                <dd>
                                  {resource.kind}/{resource.name}
                                  {resource.apiVersion !== undefined && resource.apiVersion !== ""
                                    ? ` (${resource.apiVersion})`
                                    : null}
                                </dd>
                              </div>
                              {resource.namespace !== undefined && resource.namespace !== "" ? (
                                <div>
                                  <dt>Namespace</dt>
                                  <dd>{resource.namespace}</dd>
                                </div>
                              ) : null}
                              {resource.reason !== undefined && resource.reason !== "" ? (
                                <div>
                                  <dt>Reason</dt>
                                  <dd>{resource.reason}</dd>
                                </div>
                              ) : null}
                              {resource.message !== undefined && resource.message !== "" ? (
                                <div>
                                  <dt>Message</dt>
                                  <dd>{resource.message}</dd>
                                </div>
                              ) : null}
                            </dl>
                          </li>
                        ))}
                      </ul>
                      </SectionCard>
                    </Section>
                  ) : null}
                  {task.agentOutput !== undefined && task.agentOutput !== "" ? (
                    <details>
                      <summary>Plan output</summary>
                      <Pre>{task.agentOutput}</Pre>
                    </details>
                  ) : null}
                </li>
              ))}
            </ol>
          )}
        </div>
      )}
    </QueryState>
  );
}
