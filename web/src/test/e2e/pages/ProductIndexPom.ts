import type { Locator, Page } from "@playwright/test";

export class ProductIndexPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("products-page");
  }

  get search(): Locator {
    return this.root.getByRole("search").getByLabel("Search Products");
  }

  get pagination(): Locator {
    return this.root.getByTestId("products-pagination");
  }

  get next(): Locator {
    return this.pagination.getByRole("button", { name: "Next" });
  }

  get previous(): Locator {
    return this.pagination.getByRole("button", { name: "Previous" });
  }
}
