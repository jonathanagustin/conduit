import type { Locator, Page } from "@playwright/test";

export class RiskPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("risk-page");
  }

  get search(): Locator {
    return this.root.getByRole("search").getByLabel("Search recalls");
  }

  recall(id: string): Locator {
    return this.root.getByTestId(`recall-${id}`);
  }

  get detail(): Locator {
    return this.page.getByTestId("recall-detail");
  }
}
