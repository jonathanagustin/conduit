import type { ReactNode } from "react";
import { Icon } from "@blueprintjs/core";
import type { IconName } from "@blueprintjs/icons";

import "./empty-state.css";

/** The affordance a browse surface shows when a query succeeded and returned
 * nothing -- an icon, a line naming what is absent, an optional supporting
 * sentence, and optional next steps.
 *
 * Adapted from the `empty-state` exemplar in the Foundry-UI registry, whose
 * shape generalises three captured Foundry partials (a results pane before its
 * first run, an agent's quick-prompt pane, and a pull-request list falling back
 * to recent repositories) to the centred column icon -> title -> description ->
 * actions. Ported here to the console's Blueprint tokens and class conventions.
 *
 * This is distinct from `AccessibleNonIdealState`, which `QueryState` uses for
 * the failed and loading paths: an error is a query that did not answer, an
 * empty state is a query that answered "none". Keeping them apart stops "nothing
 * matched your search" from reading like "the Hub is down". The heading level is
 * explicit for the same reason the NonIdealState adapter makes it explicit --
 * Blueprint's own primitive hard-codes an h4. */
export function EmptyState({
  icon = "search",
  title,
  description,
  action,
  headingLevel = 2,
  testId,
}: {
  icon?: IconName;
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  headingLevel?: 1 | 2;
  testId?: string;
}): ReactNode {
  const Heading = headingLevel === 1 ? "h1" : "h2";
  return (
    <div
      className="console-empty-state"
      role="status"
      {...(testId === undefined ? {} : { "data-testid": testId })}
    >
      <Icon icon={icon} size={48} className="console-empty-state__icon" />
      <Heading className="console-empty-state__title">{title}</Heading>
      {description === undefined ? null : (
        <p className="console-empty-state__description">{description}</p>
      )}
      {action === undefined ? null : <div className="console-empty-state__actions">{action}</div>}
    </div>
  );
}
