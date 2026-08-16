import type { Locator, Page } from "@playwright/test";

export class ImportPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("import-page");
  }

  get search(): Locator {
    return this.root.getByRole("search").getByLabel("Search bundle imports");
  }

  record(id: string): Locator {
    return this.root.getByTestId(`import-${id}`);
  }

  get detail(): Locator {
    return this.page.getByTestId("import-detail");
  }
}
