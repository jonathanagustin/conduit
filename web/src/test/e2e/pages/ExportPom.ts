import type { Locator, Page } from "@playwright/test";

export class ExportPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("export-page");
  }

  get search(): Locator {
    return this.root.getByRole("search").getByLabel("Search export pipelines");
  }

  pipeline(id: string): Locator {
    return this.root.getByTestId(`pipeline-${id}`);
  }

  get detail(): Locator {
    return this.page.getByTestId("pipeline-detail");
  }
}
