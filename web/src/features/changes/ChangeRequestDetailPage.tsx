import { useState } from "react";
import {
  Button,
  Callout,
  Card,
  H4,
  HTMLTable,
  TextArea,
  Tag,
  type Intent,
} from "@blueprintjs/core";
import { useParams } from "react-router-dom";

import {
  listOf,
  useChangeRequest,
  usePlan,
  useReviewChangeRequest,
  type ReviewAction,
} from "../../api/resources.js";
import { ObjectHeader } from "../../components/ObjectHeader/ObjectHeader.js";
import { QueryState } from "../../components/QueryState.js";
import "./changes.css";

const STATUS_INTENT: Record<string, Intent> = {
  PENDING_APPROVALS: "warning",
  APPROVED: "success",
  REJECTED: "danger",
  CANCELLED: "none",
  ERROR: "danger",
};

export function ChangeStatusTag({ value }: { value: string }) {
  const intent = STATUS_INTENT[value];
  return (
    // Calm states stay minimal so a FAILED or REJECTED row carries the
    // colour on a screen full of finished work.
    <Tag intent={intent ?? "none"} minimal={intent === undefined || value === "APPROVED"}>
      {value}
    </Tag>
  );
}

/** Change Request detail -- P0: "What exactly am I being asked to approve?"
 *
 * The screen has to answer that before it offers a button. Requirements say who
 * must review and whether they have; evaluations say which policy asked and
 * why; the diff says what changes. A reviewer who cannot see those is being
 * asked to approve a title. */
export function ChangeRequestDetailPage() {
  const { changeRequestId = "" } = useParams();
  const { data, error, isPending, refetch } = useChangeRequest(changeRequestId);
  const review = useReviewChangeRequest(changeRequestId);
  const [rationale, setRationale] = useState("");

  const act = (action: ReviewAction) => {
    review.mutate(
      { action, rationale },
      {
        onSuccess: () => {
          setRationale("");
        },
      },
    );
  };

  return (
    <QueryState
      isPending={isPending}
      error={error}
      data={data}
      subject={`Change Request ${changeRequestId}`}
      onRetry={() => void refetch()}
    >
      {(request) => (
        <div className="console-change" data-testid="change-request-detail">
          {/* The coordinate leads, as the plan's wireframe does ("CR-4821 ·
              Upgrade payments to 2.4.1"): the id is what an approver quotes. */}
          <ObjectHeader
            icon="inbox"
            kind="Change Request"
            title={`${request.id} · ${request.title}`}
            subtitle={`${request.environment} · opened by ${request.author.username}`}
            tags={<ChangeStatusTag value={request.status} />}
          />

          {request.description !== undefined && request.description !== "" ? (
            <p className="console-change__description">{request.description}</p>
          ) : null}

          {request.error !== undefined && request.error !== "" ? (
            <Callout intent="danger" title="This request could not be evaluated" icon="error">
              {request.error}
            </Callout>
          ) : null}

          <H4>Who must review</H4>
          {listOf(request.requirements).length === 0 ? (
            <p className="console-empty">No policy required a reviewer.</p>
          ) : (
            <HTMLTable className="console-change__table" striped>
              <thead>
                <tr>
                  <th scope="col">Requirement</th>
                  <th scope="col">Team</th>
                  <th scope="col">Reason</th>
                  <th scope="col">Satisfied by</th>
                </tr>
              </thead>
              <tbody>
                {listOf(request.requirements).map((requirement) => (
                  <tr key={requirement.id}>
                    <td>{requirement.id}</td>
                    <td>{requirement.team ?? requirement.role ?? requirement.operation ?? "—"}</td>
                    <td>{requirement.reason ?? "—"}</td>
                    <td>
                      {(requirement.satisfiedBy ?? []).length === 0 ? (
                        <Tag intent="warning" minimal round>
                          outstanding
                        </Tag>
                      ) : (
                        (requirement.satisfiedBy ?? []).join(", ")
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
          )}

          <H4>What the policies said</H4>
          {listOf(request.evaluations).length === 0 ? (
            <p className="console-empty">No policy produced an evaluation.</p>
          ) : (
            <HTMLTable className="console-change__table" striped>
              <thead>
                <tr>
                  <th scope="col">Policy</th>
                  <th scope="col">Change</th>
                  <th scope="col">Outcome</th>
                  <th scope="col">Reason</th>
                </tr>
              </thead>
              <tbody>
                {listOf(request.evaluations).map((evaluation, index) => (
                  <tr key={`${evaluation.policyId} ${evaluation.changeId} ${index}`}>
                    <td>{evaluation.policyId}</td>
                    <td>{evaluation.changeId}</td>
                    <td>{evaluation.outcome}</td>
                    <td>{evaluation.reason ?? "—"}</td>
                  </tr>
                ))}
              </tbody>
            </HTMLTable>
          )}

          <H4>What changes</H4>
          {listOf(request.changes).map((change) => (
            <Card key={change.id} className="console-change__diff">
              <div className="console-change__diff-head">
                <Tag minimal round>
                  {change.type}
                </Tag>
                <strong>{change.installationId}</strong>
              </div>
              {Object.entries(change.diff ?? {}).length === 0 ? (
                <p className="console-empty">This change records no field-level diff.</p>
              ) : (
                <HTMLTable className="console-change__table" compact striped>
                  <thead>
                    <tr>
                      <th scope="col">Field</th>
                      <th scope="col">Before</th>
                      <th scope="col">After</th>
                    </tr>
                  </thead>
                  <tbody>
                    {Object.entries(change.diff ?? {}).map(([field, entry]) => (
                      <tr key={field}>
                        <td>{field}</td>
                        <td>{format(entry.before)}</td>
                        <td>{format(entry.after)}</td>
                      </tr>
                    ))}
                  </tbody>
                </HTMLTable>
              )}
            </Card>
          ))}

          {(request.reviews ?? []).length > 0 ? (
            <>
              <H4>Reviews</H4>
              <ul className="console-change__reviews">
                {(request.reviews ?? []).map((entry, index) => (
                  <li key={`${entry.subject} ${index}`}>
                    <Tag minimal round>
                      {entry.decision}
                    </Tag>{" "}
                    {entry.subject}
                    {entry.rationale !== undefined && entry.rationale !== "" ? (
                      <> — {entry.rationale}</>
                    ) : null}
                  </li>
                ))}
              </ul>
            </>
          ) : null}

          <H4>What happens if you approve</H4>
          <Consequence planId={request.planId} />

          <H4>Review this request</H4>
          {review.error !== null ? (
            <Callout intent="danger" title="That review was not recorded" icon="error">
              {review.error.message}
            </Callout>
          ) : null}
          <TextArea
            className="console-change__rationale"
            placeholder="Why? This is recorded against your name and cannot be edited afterwards."
            value={rationale}
            onChange={(event) => setRationale(event.currentTarget.value)}
            aria-label="Rationale"
            fill
          />
          <div className="console-change__actions">
            {/* A rationale is required for every decision. The Hub records the
                review permanently, and "approved" with no reason is not an
                answer to "what exactly am I approving?". */}
            {/* Reject left, Approve rightmost: the plan's wireframe 3 orders
                them that way, and the affirming action last is the convention a
                reviewer's hand already expects. */}
            <Button
              intent="danger"
              text="Reject"
              disabled={rationale.trim() === "" || review.isPending}
              onClick={() => act("reject")}
            />
            <Button
              intent="primary"
              text="Approve Change Request"
              disabled={rationale.trim() === "" || review.isPending}
              onClick={() => act("approve")}
            />
            <Button
              text="Cancel request"
              disabled={rationale.trim() === "" || review.isPending}
              onClick={() => act("cancel")}
            />
            <Button
              text="Reopen"
              disabled={rationale.trim() === "" || review.isPending}
              onClick={() => act("reopen")}
            />
          </div>
        </div>
      )}
    </QueryState>
  );
}

function format(value: unknown): string {
  if (value === undefined || value === null) {
    return "—";
  }
  if (typeof value === "string") {
    return value === "" ? "—" : value;
  }
  return JSON.stringify(value);
}

/** The consequence of approving, which the plan's wireframe puts beside the
 * approval requirements: "Creates Plan with 2 steps / Downtime: none".
 *
 * A reviewer who cannot see this is approving a title and a diff. The Hub does
 * not publish a blast-radius summary, so what is shown is what it does publish
 * -- the Plan this request governs and the steps it will execute -- and the
 * absence of anything more is stated rather than papered over. */
function Consequence({ planId }: { planId: string }) {
  const { data, error, isPending } = usePlan(planId);

  if (planId === "") {
    return (
      <Callout icon="info-sign" compact>
        This request governs no Plan, so approving it executes nothing directly.
      </Callout>
    );
  }
  if (isPending) {
    return <p className="console-empty">Reading the Plan this request governs…</p>;
  }
  if (error !== null || data === undefined) {
    // Not knowing the consequence is itself worth saying, loudly: it is the
    // difference between an informed approval and a blind one.
    return (
      <Callout intent="warning" icon="warning-sign" compact>
        The Plan this request governs could not be read, so the consequence of approving is
        unknown here. {error?.message ?? ""}
      </Callout>
    );
  }
  return (
    <Callout icon="gantt-chart" compact>
      Approving releases <strong>{data.id}</strong> in {data.plan.environment}:{" "}
      {data.plan.steps.length === 0
        ? "no steps"
        : `${data.plan.steps.length} step${data.plan.steps.length === 1 ? "" : "s"}`}
      {data.plan.steps.length > 0 ? (
        <> &mdash; {data.plan.steps.map((step) => step.installationId).join(", ")}.</>
      ) : (
        "."
      )}{" "}
      The Hub publishes no downtime estimate, so none is shown.
    </Callout>
  );
}
