import type { Locator, Page } from "@playwright/test";

/** Plan-detail locators speak in execution tasks and evidence, not DOM classes. */
export class PlanDetailPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("plan-detail");
  }

  task(phase: string): Locator {
    return this.root.getByRole("listitem", { name: `${phase} task`, exact: true });
  }

  taskBySequence(sequence: number): Locator {
    return this.root.getByTestId(`plan-task-${sequence}`);
  }

  resourceDiagnostics(phase: string): Locator {
    return this.task(phase).getByRole("list", {
      name: `Resource diagnostics for ${phase}`,
      exact: true,
    });
  }

  resourceDiagnosticsBySequence(sequence: number): Locator {
    return this.taskBySequence(sequence).getByTestId(`resource-diagnostics-${sequence}`);
  }

  resource(phase: string, coordinate: string): Locator {
    return this.resourceDiagnostics(phase).getByRole("listitem").filter({ hasText: coordinate });
  }

  get planOutput(): Locator {
    return this.root.getByText("Plan output", { exact: true });
  }
}
