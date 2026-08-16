import type { Locator, Page } from "@playwright/test";

export class TeamDetailPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("team-detail");
  }

  get admins(): Locator {
    return this.page.getByTestId("team-admins");
  }

  get members(): Locator {
    return this.page.getByTestId("team-members");
  }
}
