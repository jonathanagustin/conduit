import type { Locator, Page } from "@playwright/test";

/** Operator interactions and stable structure for one Environment detail view. */
export class EnvironmentDetailPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("environment-detail");
  }

  get overviewSummary(): Locator {
    return this.root.getByTestId("environment-overview-summary");
  }

  tab(name: "Overview" | "Plans" | "Commands"): Locator {
    return this.root.getByRole("tab", { name, exact: true });
  }

  entityRow(installationId: string): Locator {
    return this.root.getByTestId(`environment-entity-row-${installationId}`);
  }

  entityCard(installationId: string): Locator {
    return this.root.getByTestId(`environment-entity-card-${installationId}`);
  }

  planRow(planId: string): Locator {
    return this.root.getByTestId(`environment-plan-row-${planId}`);
  }

  planCard(planId: string): Locator {
    return this.root.getByTestId(`environment-plan-card-${planId}`);
  }

  planLink(planId: string): Locator {
    return this.root.getByRole("link", { name: planId, exact: true });
  }

  commandRow(planId: string): Locator {
    return this.root.getByTestId(`environment-command-row-${planId}`);
  }

  commandCard(planId: string): Locator {
    return this.root.getByTestId(`environment-command-card-${planId}`);
  }
}
