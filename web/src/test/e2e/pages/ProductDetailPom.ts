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

  /** The fixed resource-summary pane that heads the Product browser. */
  get resourceSummary(): Locator {
    return this.root.getByTestId("product-resource-summary");
  }

  /** The fluid work column the resource summary is seated beside. */
  get work(): Locator {
    return this.root.getByTestId("product-work");
  }

  /** The dense Release/install table in the work column. */
  get releaseTable(): Locator {
    return this.root.getByRole("table").first();
  }

  /** The pipeline panel as a real Blueprint Section (its 4px corner and
   * hairline come from the primitive, not from a styled div). */
  get pipelineCard(): Locator {
    return this.root.locator(".console-products__pipeline.bp6-section");
  }

  get assessmentCard(): Locator {
    return this.root.locator(".console-products__assessment.bp6-section");
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

  assessmentToggle(version: string): Locator {
    return this.root.getByTestId(`release-assessment-${version}`);
  }

  get assessmentDetail(): Locator {
    return this.root.getByTestId("release-assessment-detail");
  }

  get activeFindings(): Locator {
    return this.assessmentDetail.getByTestId("release-active-findings");
  }

  get suppressedFindings(): Locator {
    return this.assessmentDetail.getByTestId("release-suppressed-findings");
  }

  finding(cve: string): Locator {
    return this.assessmentDetail.getByTestId(`finding-${cve}`);
  }

  suppressed(cve: string): Locator {
    return this.assessmentDetail.getByTestId(`suppressed-${cve}`);
  }
}
