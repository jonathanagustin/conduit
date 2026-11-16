import { Button, Callout, NonIdealState } from "@blueprintjs/core";
import type { ReactNode } from "react";

import "./conduit-primitives.css";

export type ViewStateKind =
  | "loading"
  | "ready"
  | "empty"
  | "stale"
  | "partial-failure"
  | "permission-denied"
  | "disconnected";

export interface ViewStateBoundaryProps {
  readonly state: ViewStateKind;
  readonly testId?: string;
  readonly loadingLabel?: string;
  readonly title?: string;
  readonly description?: ReactNode;
  readonly onRetry?: () => void;
  readonly retryLabel?: string;
  readonly children?: ReactNode;
}

export function ViewStateBoundary({
  state,
  testId,
  loadingLabel = "Loading…",
  title,
  description,
  onRetry,
  retryLabel = "Retry",
  children,
}: ViewStateBoundaryProps) {
  if (state === "ready") {
    return <>{children}</>;
  }

  if (state === "loading") {
    return (
      <div
        className="cnd-view-boundary cnd-view-loading"
        role="status"
        aria-busy="true"
        data-testid={testId}
      >
        <span className="cnd-loading-label">{loadingLabel}</span>
        <span
          className="cnd-skeleton-bar"
          style={{ width: "38%", height: 22 }}
        />
        <span
          className="cnd-skeleton-bar"
          style={{ width: "85%", height: 16 }}
        />
        <span
          className="cnd-skeleton-bar"
          style={{ width: "65%", height: 16 }}
        />
        <span
          className="cnd-skeleton-bar"
          style={{ width: "100%", height: 80 }}
        />
      </div>
    );
  }

  if (
    state === "empty" ||
    state === "permission-denied" ||
    state === "disconnected"
  ) {
    const isAlert = state === "disconnected" || state === "permission-denied";
    const defaultTitle =
      state === "empty"
        ? "No records found"
        : state === "permission-denied"
          ? "Permission denied"
          : "Site disconnected";

    const retryAction = onRetry ? (
      <Button
        intent={state === "permission-denied" ? "none" : "primary"}
        onClick={onRetry}
        text={retryLabel}
      />
    ) : undefined;

    return (
      <div
        className="cnd-view-boundary cnd-view-non-ideal"
        role={isAlert ? "alert" : "status"}
        data-testid={testId}
      >
        <NonIdealState
          title={title ?? defaultTitle}
          description={description}
          action={retryAction}
        />
      </div>
    );
  }

  if (state === "stale" || state === "partial-failure") {
    const isAlert = state === "partial-failure";
    const intent = state === "stale" ? "warning" : "danger";
    const defaultTitle =
      state === "stale" ? "Data may be out of date" : "Partial failure";

    const retryAction = onRetry ? (
      <Button
        minimal
        intent={intent}
        onClick={onRetry}
        text={retryLabel}
        style={{ marginTop: 8 }}
      />
    ) : undefined;

    return (
      <div className="cnd-view-boundary" data-testid={testId}>
        <Callout
          className="cnd-view-callout-top"
          intent={intent}
          role={isAlert ? "alert" : "status"}
          title={title ?? defaultTitle}
        >
          {description ? <div>{description}</div> : null}
          {retryAction}
        </Callout>
        {children}
      </div>
    );
  }

  return <>{children}</>;
}
