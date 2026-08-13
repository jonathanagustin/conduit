import { Classes, NonIdealState, type NonIdealStateProps } from "@blueprintjs/core";
import type { ReactNode } from "react";

/** Blueprint's NonIdealState hard-codes an h4. This adapter preserves its
 * visual primitive while making the semantic heading level explicit. */
export function AccessibleNonIdealState({
  icon,
  title,
  description,
  action,
  headingLevel = 2,
}: {
  icon?: NonIdealStateProps["icon"];
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  headingLevel?: 1 | 2;
}) {
  const Heading = headingLevel === 1 ? "h1" : "h2";
  return (
    <NonIdealState {...(icon === undefined ? {} : { icon })}>
      <div className={Classes.NON_IDEAL_STATE_TEXT}>
        <Heading className={Classes.HEADING}>{title}</Heading>
        {description === undefined ? null : <div>{description}</div>}
      </div>
      {action}
    </NonIdealState>
  );
}
