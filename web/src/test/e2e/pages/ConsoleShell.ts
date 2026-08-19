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

  /** The pinned collapse/expand toggle. Present only when the rail is toggleable
   * (a viewport wide enough not to force the collapsed state). */
  get collapseToggle(): Locator {
    return this.globalNavigation.getByRole("button", { name: /navigation$/ });
  }

  /** The primary-destinations group of the expanded rail. */
  get primaryGroup(): Locator {
    return this.page.getByTestId("rail-primary");
  }

  /** The Applications group of the expanded rail (labeled rows, not the popover). */
  get applicationsGroup(): Locator {
    return this.page.getByTestId("rail-applications");
  }

  /** A primary destination as an expanded labeled row. */
  primaryLink(name: string): Locator {
    return this.primaryGroup.getByRole("link", { name, exact: true });
  }

  /** An application as an expanded labeled row. */
  applicationLink(name: string): Locator {
    return this.applicationsGroup.getByRole("link", { name, exact: true });
  }

  /** The Applications popover trigger of the collapsed rail. */
  get applicationsButton(): Locator {
    return this.page.getByRole("button", { name: "Applications" });
  }

  /** An application inside the collapsed rail's Applications popover. */
  application(name: string): Locator {
    return this.page.getByRole("menuitem", { name, exact: true });
  }

  get capabilitiesUnavailable(): Locator {
    return this.page.getByTestId("session-capabilities-unavailable");
  }
}
