import type { ReactNode } from "react";
import { AppHeader, type AppHeaderProps } from "../AppHeader/AppHeader.js";
import "./app-frame.css";

/** Full-height application frame: header above, body filling the rest.
 *
 * @provenance ported from packages/foundry/packages/foundry-ui, a tenant-neutral
 *   Foundry reconstruction, Blueprint 5.19.1 -> 6.18.0. */
export interface AppFrameProps extends AppHeaderProps {
  children: ReactNode;
  className?: string;
  bodyClassName?: string;
  testId?: string;
}

export function AppFrame({
  children,
  className = "",
  bodyClassName = "",
  testId = "app-frame",
  ...header
}: AppFrameProps) {
  return (
    <div
      className={["console-app-frame", className].filter(Boolean).join(" ")}
      data-testid={testId}
    >
      <a className="console-app-frame__skip-link" href="#main-content">
        Skip to content
      </a>
      <AppHeader {...header} testId={`${testId}-header`} />
      <main
        id="main-content"
        tabIndex={-1}
        className={["console-app-frame__body", bodyClassName].filter(Boolean).join(" ")}
      >
        {children}
      </main>
    </div>
  );
}
