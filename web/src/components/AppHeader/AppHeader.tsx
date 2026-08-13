import { Icon, Navbar, type IconName } from "@blueprintjs/core";
import type { ReactNode } from "react";
import { AppLogo } from "../AppLogo/AppLogo.js";
import "./app-header.css";

/** The 50px application header: identity mark, titles, optional resource tabs,
 * search and actions. The host injects identity explicitly rather than reading a
 * product-specific context, so one component serves every surface.
 *
 * @provenance ported from packages/foundry/packages/foundry-ui, a tenant-neutral
 *   Foundry reconstruction, Blueprint 5.19.1 -> 6.18.0. The upstream Apollo
 *   header composition it is used for -- icon, primary name, resource kind,
 *   status tags, team context, Actions menu at the right -- is described in
 *   docs/ui/CONTROL-PLANE-UI-PLAN.md and evidenced by the hash-anchored captures
 *   in docs/ui/upstream-screen-evidence.tsv. No private DOM or CSS claim. */
export interface AppHeaderProps {
  variant?: "identity" | "compact";
  icon?: IconName;
  identityIcon?: ReactNode;
  accent?: string;
  title?: ReactNode;
  subtitle?: ReactNode;
  tabs?: ReactNode;
  lead?: ReactNode;
  search?: ReactNode;
  actions?: ReactNode;
  testId?: string;
}

export function AppHeader({
  variant = "identity",
  icon = "application",
  identityIcon,
  accent,
  title = "Platform",
  subtitle,
  tabs,
  lead,
  search,
  actions,
  testId = "app-header",
}: AppHeaderProps) {
  return (
    <Navbar
      className={`console-app-header console-app-header--${variant}`}
      data-testid={testId}
      role="banner"
    >
      {variant === "identity" ? (
        (identityIcon ?? (
          <AppLogo
            icon={icon}
            {...(accent === undefined ? {} : { accent })}
            size={50}
            testId={`${testId}-logo`}
          />
        ))
      ) : (
        <Icon className="console-app-header__compact-icon" icon={icon} size={28} />
      )}
      <span className="console-app-header__titles">
        <b>{title}</b>
        {subtitle != null ? <small>{subtitle}</small> : null}
      </span>
      {tabs != null ? (
        <nav className="console-app-header__tabs" aria-label="Resource sections">
          {tabs}
        </nav>
      ) : null}
      {lead != null ? <div className="console-app-header__lead">{lead}</div> : null}
      {search != null ? <div className="console-app-header__search">{search}</div> : null}
      {actions != null ? <div className="console-app-header__actions">{actions}</div> : null}
    </Navbar>
  );
}
