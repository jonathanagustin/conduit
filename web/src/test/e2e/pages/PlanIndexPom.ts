import type { Locator, Page } from "@playwright/test";

export class PlanIndexPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("plans-page");
  }

  get search(): Locator {
    return this.root.getByRole("search").getByLabel("Search Plans");
  }

  get pagination(): Locator {
    return this.root.getByTestId("plans-pagination");
  }

  get next(): Locator {
    return this.pagination.getByRole("button", { name: "Next" });
  }

  get previous(): Locator {
    return this.pagination.getByRole("button", { name: "Previous" });
  }

  get retry(): Locator {
    return this.page.getByRole("button", { name: "Try again" });
  }
}
