import type { Locator, Page } from "@playwright/test";

/** Product-detail locators preserve Product, Release, and channel terminology. */
export class ProductDetailPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("product-detail");
  }

  release(version: string): Locator {
    return this.root.getByTestId(`release-${version}`);
  }

  get promotionPipeline(): Locator {
    return this.root.getByTestId("release-channel-pipeline");
  }

  promotionTransition(from: string, to: string): Locator {
    return this.promotionPipeline.getByRole("button", {
      name: new RegExp(`^${from} to ${to}:`),
    });
  }

  promotionTransitionById(id: string): Locator {
    return this.promotionPipeline.getByTestId(`promotion-transition-${id}`);
  }

  promotionConstraints(from: string, to: string): Locator {
    return this.root.getByRole("region", {
      name: `${from} to ${to} promotion constraints`,
      exact: true,
    });
  }

  promotionConstraintsById(id: string): Locator {
    return this.root.getByTestId(`promotion-constraints-${id}`);
  }
}
