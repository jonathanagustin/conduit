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

  get rail(): Locator {
    return this.root.getByTestId("facet-rail");
  }

  /** The fluid work column the rail is seated beside. */
  get results(): Locator {
    return this.root.getByTestId("fleet-results");
  }

  get table(): Locator {
    return this.results.getByRole("table");
  }

  /** One dimension of the rail, named by the query parameter it selects on --
   * which is what the Hub serves the facet under, so a test cannot ask about a
   * facet by a name the filter would not accept. */
  facet(key: string): Locator {
    return this.rail.getByTestId(`facet-${key}`);
  }

  /** A facet value, matched on the whole accessible name -- the value followed
   * by its count. Anchored and escaped because a substring match would make
   * HEALTHY find UNHEALTHY, and a Product name is full of regexp punctuation. */
  facetValue(key: string, value: string, count?: number): Locator {
    const escaped = value.replaceAll(/[.*+?^${}()|[\]\\]/g, String.raw`\$&`);
    const suffix = count === undefined ? String.raw`\s+\d+` : `\\s+${count}`;
    return this.facet(key).getByRole("button", { name: new RegExp(`^${escaped}${suffix}$`) });
  }
}
