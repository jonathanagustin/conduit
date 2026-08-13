import type { Locator, Page } from "@playwright/test";

/** Fleet locators anchored to the domain screen and its paging boundary. */
export class FleetPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("fleet-page");
  }

  get pagination(): Locator {
    return this.root.getByTestId("fleet-pagination");
  }

  get search(): Locator {
    return this.root.getByRole("search").getByLabel("Search installations");
  }

  get next(): Locator {
    return this.pagination.getByRole("button", { name: "Next" });
  }

  get previous(): Locator {
    return this.pagination.getByRole("button", { name: "Previous" });
  }
}
