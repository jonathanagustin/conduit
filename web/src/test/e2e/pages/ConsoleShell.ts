import type { Locator, Page } from "@playwright/test";

/** Stable operator-level locators for chrome shared by every console route. */
export class ConsoleShell {
  constructor(private readonly page: Page) {}

  get main(): Locator {
    return this.page.getByRole("main");
  }

  get skipLink(): Locator {
    return this.page.getByRole("link", { name: "Skip to content" });
  }

  get globalNavigation(): Locator {
    return this.page.getByRole("navigation", { name: "Global navigation" });
  }

  get workspaceCanvas(): Locator {
    return this.page.getByTestId("console-workspace-canvas");
  }

  get applicationsButton(): Locator {
    return this.page.getByRole("button", { name: "Applications" });
  }

  application(name: string): Locator {
    return this.page.getByRole("menuitem", { name, exact: true });
  }

  get capabilitiesUnavailable(): Locator {
    return this.page.getByTestId("session-capabilities-unavailable");
  }
}
