import type { Locator, Page } from "@playwright/test";

export class TeamIndexPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("teams-page");
  }

  get search(): Locator {
    return this.root.getByRole("search").getByLabel("Search Teams");
  }

  row(name: string): Locator {
    return this.root.getByRole("row", { name: new RegExp(name) });
  }
}
