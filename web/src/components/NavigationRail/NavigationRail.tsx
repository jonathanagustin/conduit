import { Icon, Menu, MenuItem, PopoverNext, Tooltip, type IconName } from "@blueprintjs/core";
import { useRef, type Ref } from "react";
import { NavLink, useHref } from "react-router-dom";
import "./navigation-rail.css";

/** The narrow dark rail down the left edge carrying global navigation, with an
 * Applications popover for the surfaces that are not part of the operating loop.
 *
 * The rail and the popover are both upstream composition: the plan describes "a
 * narrow dark icon rail for global navigation and an Applications popover for
 * Change Requests, Entity Map, Export, Import, and Risk Management", and capture
 * `060-...` in docs/ui/upstream-screen-evidence.tsv is the Applications menu
 * itself. Structure only -- no upstream DOM, class names or CSS are reproduced. */
export interface RailDestination {
  id: string;
  label: string;
  icon: IconName;
  to: string;
}

export interface NavigationRailProps {
  primary: RailDestination[];
  applications: RailDestination[];
}

export function NavigationRail({ primary, applications }: NavigationRailProps) {
  const firstApplication = useRef<HTMLLIElement>(null);
  return (
    <nav className="console-rail bp6-dark" aria-label="Global navigation">
      {primary.map((destination) => (
        <Tooltip
          key={destination.id}
          content={destination.label}
          placement="right"
          hoverOpenDelay={200}
          renderTarget={({ ref, className, ...targetProps }) => (
            <NavLink
              {...targetProps}
              ref={ref as Ref<HTMLAnchorElement>}
              to={destination.to}
              // `end` scopes the match to this route and its children only. Without
              // it every rail item whose path is a prefix lights up -- Home stayed
              // active on every screen, because "/" prefixes everything -- and a
              // rail that says you are somewhere you are not is worse than one
              // that says nothing.
              end={destination.to === "/overview"}
              className={({ isActive }) =>
                [className, "console-rail__item", isActive && "console-rail__item--active"]
                  .filter(Boolean)
                  .join(" ")
              }
              aria-label={destination.label}
            >
              <Icon icon={destination.icon} size={18} />
            </NavLink>
          )}
        />
      ))}
      <div className="console-rail__spacer" />
      {applications.length > 0 ? (
        <PopoverNext
          autoFocus
          onOpened={() => firstApplication.current?.querySelector<HTMLElement>("[role=menuitem]")?.focus()}
          placement="right-end"
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
              className={
                isOpen ? "console-rail__item console-rail__item--active" : "console-rail__item"
              }
            >
              <Icon icon="grid" size={18} />
            </button>
          )}
        />
      ) : null}
    </nav>
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
