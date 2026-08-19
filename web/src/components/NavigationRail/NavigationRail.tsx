import {
  Button,
  Divider,
  Icon,
  Menu,
  MenuItem,
  PopoverNext,
  Text,
  Tooltip,
  type IconName,
} from "@blueprintjs/core";
import { useRef, type Ref } from "react";
import { NavLink, useHref } from "react-router-dom";
import "./navigation-rail.css";

/** The dark rail down the left edge carrying global navigation, in two states.
 *
 * The rail and the Applications popover are both upstream composition: the plan
 * describes "a narrow dark icon rail for global navigation and an Applications
 * popover for Change Requests, Entity Map, Export, Import, and Risk Management",
 * and capture `060-...` in docs/ui/upstream-screen-evidence.tsv is the
 * Applications menu itself. Structure only -- no upstream DOM, class names or
 * CSS are reproduced.
 *
 * The Foundry platform sidebar is a two-state control, and this rail follows it:
 *
 *   - Expanded (the shell's `--console-rail` width, in the rulebook's 230-260px
 *     band): labeled rows, the Applications section under a group heading, the
 *     collapse toggle pinned above a scrolling middle. This is the
 *     browser-archetype default -- a labeled rail teaches the product's shape,
 *     which an icon-only rail cannot until the operator has learned every glyph.
 *   - Collapsed (48px): the icon rail, labels and headings gone, per-item
 *     tooltips restored, the Applications section folded back into its popover.
 *     Collapse changes information density, not merely width.
 *
 * The component is controlled: the collapsed choice and the responsive
 * force-collapse live in `app/` (the composition layer), so this stays a
 * presentation primitive that is told a state and reports a toggle. When
 * `onToggleCollapsed` is absent the toggle is not offered -- the state is being
 * forced (a narrow viewport) and cannot be changed here. */
export interface RailDestination {
  id: string;
  label: string;
  icon: IconName;
  to: string;
}

export interface NavigationRailProps {
  primary: RailDestination[];
  applications: RailDestination[];
  /** Whether the rail is rendered as the 48px icon rail rather than the labeled
   * expanded rail. */
  collapsed: boolean;
  /** Flip the collapsed choice. Absent when the state is forced (narrow
   * viewport), which is what hides the toggle. */
  onToggleCollapsed?: () => void;
}

export function NavigationRail({
  primary,
  applications,
  collapsed,
  onToggleCollapsed,
}: NavigationRailProps) {
  return (
    <nav
      className="console-rail bp6-dark"
      aria-label="Global navigation"
      {...(collapsed ? { "data-collapsed": "true" } : {})}
    >
      {onToggleCollapsed ? (
        <div className="console-rail__header">
          <Tooltip
            content={collapsed ? "Expand navigation" : "Collapse navigation"}
            placement="right"
            hoverOpenDelay={200}
          >
            <Button
              className="console-rail__toggle"
              variant="minimal"
              size="small"
              icon={collapsed ? "double-chevron-right" : "double-chevron-left"}
              aria-label={collapsed ? "Expand navigation" : "Collapse navigation"}
              aria-expanded={!collapsed}
              onClick={onToggleCollapsed}
            />
          </Tooltip>
        </div>
      ) : null}
      <div className="console-rail__scroll">
        <RailGroup
          kind="primary"
          destinations={primary}
          collapsed={collapsed}
          testId="rail-primary"
        />
        {applications.length > 0 ? (
          <>
            <Divider className="console-rail__divider" />
            {collapsed ? (
              <ApplicationsPopover applications={applications} />
            ) : (
              <RailGroup
                kind="application"
                heading="Applications"
                destinations={applications}
                collapsed={false}
                testId="rail-applications"
              />
            )}
          </>
        ) : null}
      </div>
    </nav>
  );
}

function RailGroup({
  kind,
  destinations,
  collapsed,
  heading,
  testId,
}: {
  kind: "primary" | "application";
  destinations: RailDestination[];
  collapsed: boolean;
  heading?: string;
  testId: string;
}) {
  return (
    <div className={`console-rail__group console-rail__group--${kind}`} data-testid={testId}>
      {heading && !collapsed ? <Text className="console-rail__heading">{heading}</Text> : null}
      <ul className="console-rail__list">
        {destinations.map((destination) => (
          <li key={destination.id} className="console-rail__row">
            <RailRow destination={destination} collapsed={collapsed} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function RailRow({ destination, collapsed }: { destination: RailDestination; collapsed: boolean }) {
  // `end` scopes the match to this route and its children only. Without it every
  // rail item whose path is a prefix lights up -- Home stayed active on every
  // screen, because "/" prefixes everything -- and a rail that says you are
  // somewhere you are not is worse than one that says nothing.
  const end = destination.to === "/overview";
  const itemClassName = ({ isActive }: { isActive: boolean }) =>
    ["console-rail__item", isActive && "console-rail__item--active"].filter(Boolean).join(" ");

  // Expanded: the label is visible, so a tooltip would only repeat it. The
  // leading icon is 16px, then a 12px gap to the ellipsized label (min-width:0).
  if (!collapsed) {
    return (
      <NavLink to={destination.to} end={end} className={itemClassName} aria-label={destination.label}>
        <Icon icon={destination.icon} size={16} />
        <Text ellipsize className="console-rail__label">
          {destination.label}
        </Text>
      </NavLink>
    );
  }

  // Collapsed: the label is gone, so the tooltip is how the destination is still
  // named to a pointer, and the aria-label is how it is named to a reader.
  return (
    <Tooltip
      content={destination.label}
      placement="right"
      hoverOpenDelay={200}
      renderTarget={({ ref, className: tooltipClassName, ...targetProps }) => (
        <NavLink
          {...targetProps}
          ref={ref as Ref<HTMLAnchorElement>}
          to={destination.to}
          end={end}
          className={({ isActive }) =>
            [tooltipClassName, itemClassName({ isActive })].filter(Boolean).join(" ")
          }
          aria-label={destination.label}
        >
          <Icon icon={destination.icon} size={18} />
        </NavLink>
      )}
    />
  );
}

function ApplicationsPopover({ applications }: { applications: RailDestination[] }) {
  const firstApplication = useRef<HTMLLIElement>(null);
  return (
    <PopoverNext
      autoFocus
      onOpened={() => firstApplication.current?.querySelector<HTMLElement>("[role=menuitem]")?.focus()}
      placement="right-start"
      shouldReturnFocusOnClose
      content={
        <Menu>
          {applications.map((application, index) => (
            <ApplicationMenuItem
              key={application.id}
              application={application}
              {...(index === 0 ? { itemRef: firstApplication } : {})}
            />
          ))}
        </Menu>
      }
      renderTarget={({ isOpen, ref, ...targetProps }) => (
        <button
          {...targetProps}
          ref={ref}
          type="button"
          aria-label="Applications"
          aria-expanded={isOpen}
          className="console-rail__item console-rail__item--applications"
        >
          <Icon icon="grid" size={18} />
        </button>
      )}
    />
  );
}

function ApplicationMenuItem({
  application,
  itemRef,
}: {
  application: RailDestination;
  itemRef?: Ref<HTMLLIElement>;
}) {
  const href = useHref(application.to);
  return <MenuItem ref={itemRef} icon={application.icon} text={application.label} href={href} />;
}
