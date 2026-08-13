import type { Locator, Page } from "@playwright/test";

/** Fleet-wide promotion locators keyed by the immutable Promotion identity. */
export class PromotionsPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("promotions-page");
  }

  transition(id: string): Locator {
    return this.root.getByTestId(`promotion-transition-${id}`);
  }

  constraints(id: string): Locator {
    return this.root.getByTestId(`promotion-constraints-${id}`);
  }
}
