import { EntityTitle, Icon, Menu, MenuItem, Popover, type IconName } from "@blueprintjs/core";
import type { ReactNode } from "react";

import { AppLogo } from "../AppLogo/AppLogo.js";
import "./object-header.css";

/** The resource identity row: icon, kind, name, status tags, Actions.
 *
 * @provenance ported from packages/foundry/packages/foundry-ui
 *   ObjectDetails/ObjectHeader, a tenant-neutral Foundry reconstruction, from
 *   Blueprint 5.19.1 to 6.18.0. The star affordance is dropped -- this console
 *   has no favourites -- and a `kind` line is added because the plan's recurring
 *   composition names "icon, primary name, resource kind, status tags, team
 *   context, and an Actions menu at the right", and upstream's own header
 *   carries the kind. Everything else, including the EntityTitle composition, is
 *   the foundry-ui structure.
 *
 * Written by hand rather than imported: @foundry/foundry-ui is a private
 * workspace package peered to Blueprint 5.19.1 and React 18.2.0, and platform/
 * is a standalone repository that cannot resolve a workspace sibling. */
export interface ResourceAction {
  id: string;
  text: string;
  icon?: IconName;
  onClick: () => void;
  disabled?: boolean;
}

export interface ObjectHeaderProps {
  icon: IconName;
  accent?: string;
  kind: string;
  title: ReactNode;
  subtitle?: ReactNode;
  tags?: ReactNode;
  actions?: ResourceAction[];
  testId?: string;
}

export function ObjectHeader({
  icon,
  accent = "#2d72d2",
  kind,
  title,
  subtitle,
  tags,
  actions = [],
  testId = "object-header",
}: ObjectHeaderProps) {
  return (
    <header className="console-object-header" data-testid={testId}>
      <AppLogo icon={icon} accent={accent} size={42} testId={`${testId}-icon`} />
      <div className="console-object-header__entity">
        <div className="console-object-header__kind">{kind}</div>
        <EntityTitle
          title={<h1 className="console-object-header__title">{title}</h1>}
          {...(subtitle === undefined
            ? {}
            : { subtitle: typeof subtitle === "string" ? subtitle : <>{subtitle}</> })}
        />
      </div>
      {tags !== undefined ? <div className="console-object-header__tags">{tags}</div> : null}
      {actions.length > 0 ? (
        <div className="console-object-header__actions">
          <Popover
            placement="bottom-end"
            content={
              <Menu>
                {actions.map((action) => (
                  <MenuItem
                    key={action.id}
                    text={action.text}
                    {...(action.icon === undefined ? {} : { icon: action.icon })}
                    disabled={action.disabled === true}
                    onClick={action.onClick}
                  />
                ))}
              </Menu>
            }
            // A native button carrying Blueprint's classes: Popover's
            // renderTarget hands back props whose optional handlers are
            // `T | undefined`, which exactOptionalPropertyTypes will not spread
            // onto Blueprint's own stricter prop types.
            renderTarget={({ isOpen, ref, ...targetProps }) => (
              <button
                {...targetProps}
                ref={ref}
                type="button"
                className={`bp6-button${isOpen ? " bp6-active" : ""}`}
                aria-expanded={isOpen}
              >
                Actions
                <Icon icon="caret-down" />
              </button>
            )}
          />
        </div>
      ) : null}
    </header>
  );
}
