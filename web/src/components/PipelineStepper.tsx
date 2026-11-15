import type { ReactNode } from "react";

import "./conduit-primitives.css";

export type PipelineStepState =
  | "done"
  | "active"
  | "failed"
  | "blocked"
  | "waiting";

export interface PipelineStep {
  readonly id: string;
  readonly label: string;
  readonly state: PipelineStepState;
  readonly detail?: string;
}

export interface PipelineStepperProps {
  readonly steps: readonly PipelineStep[];
  readonly ariaLabel?: string;
  readonly className?: string;
}

function renderStepMark(state: PipelineStepState, index: number): string {
  switch (state) {
    case "done":
      return "✓";
    case "failed":
      return "✕";
    case "blocked":
      return "!";
    default:
      return String(index + 1);
  }
}

export function PipelineStepper({
  steps,
  ariaLabel = "Pipeline stages",
  className,
}: PipelineStepperProps) {
  return (
    <nav
      className={["cnd-pipeline-stepper", className ?? ""]
        .filter(Boolean)
        .join(" ")}
      aria-label={ariaLabel}
    >
      {steps.map((step, index) => {
        const isDone = step.state === "done";
        const isActive = step.state === "active";
        const isFailed = step.state === "failed";
        const isBlocked = step.state === "blocked";

        const stateClass = isDone
          ? "is-done"
          : isActive
            ? "is-active"
            : isFailed
              ? "is-failed"
              : isBlocked
                ? "is-blocked"
                : "is-waiting";

        return (
          <div key={step.id} className="cnd-pipeline-step-item">
            <div
              className={`cnd-pipeline-step ${stateClass}`}
              aria-current={isActive ? "step" : undefined}
            >
              <span className="cnd-pipeline-step-indicator">
                {renderStepMark(step.state, index)}
              </span>
              <span className="cnd-pipeline-step-label">{step.label}</span>
              {step.detail ? (
                <span className="cnd-pipeline-step-detail">
                  ({step.detail})
                </span>
              ) : null}
            </div>
            {index < steps.length - 1 ? (
              <span
                className={`cnd-pipeline-connector ${isDone ? "is-done" : ""}`}
                aria-hidden="true"
              />
            ) : null}
          </div>
        );
      })}
    </nav>
  );
}

export interface DigestPillProps {
  readonly value: string;
  readonly label?: string;
  readonly className?: string;
}

export function DigestPill({ value, label, className }: DigestPillProps) {
  return (
    <span
      className={["cnd-digest-pill", className ?? ""].filter(Boolean).join(" ")}
      title={value}
    >
      {label ? <span className="cnd-digest-pill-label">{label}</span> : null}
      <span className="cnd-digest-pill-value">{value}</span>
    </span>
  );
}

export interface MetadataGridItem {
  readonly label: string;
  readonly value: ReactNode;
  readonly mono?: boolean;
}

export interface MetadataGridProps {
  readonly items: readonly MetadataGridItem[];
  readonly className?: string;
}

export function MetadataGrid({ items, className }: MetadataGridProps) {
  return (
    <dl
      className={["cnd-meta-grid", className ?? ""].filter(Boolean).join(" ")}
    >
      {items.map((item) => (
        <div key={item.label} className="cnd-meta-card">
          <dt className="cnd-meta-label">{item.label}</dt>
          <dd className={`cnd-meta-value ${item.mono ? "is-mono" : ""}`.trim()}>
            {item.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}
