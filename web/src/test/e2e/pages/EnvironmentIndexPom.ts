import type { Locator, Page } from "@playwright/test";

export class EnvironmentIndexPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("environments-page");
  }

  get search(): Locator {
    return this.root.getByRole("search").getByLabel("Search Environments");
  }

  get pagination(): Locator {
    return this.root.getByTestId("environments-pagination");
  }

  get next(): Locator {
    return this.pagination.getByRole("button", { name: "Next" });
  }

  get previous(): Locator {
    return this.pagination.getByRole("button", { name: "Previous" });
  }
}
