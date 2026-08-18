import type { Locator, Page } from "@playwright/test";

/** Operator-level locators for the notification inbox and the preference editor
 * beneath it. */
export class NotificationsPom {
  constructor(private readonly page: Page) {}

  get root(): Locator {
    return this.page.getByTestId("notifications-page");
  }

  get channel(): Locator {
    return this.root.getByLabel("Inbox channel");
  }

  entry(eventId: string): Locator {
    return this.root.getByTestId(`inbox-entry-${eventId}`);
  }

  get empty(): Locator {
    return this.root.getByTestId("inbox-empty");
  }

  get bounded(): Locator {
    return this.root.getByTestId("inbox-bounded");
  }

  get resource(): Locator {
    return this.root.getByLabel("Resource RID");
  }

  get loadPreference(): Locator {
    return this.root.getByRole("button", { name: "Load preference" });
  }

  get editor(): Locator {
    return this.root.getByTestId("preference-editor");
  }

  get preferenceAbsent(): Locator {
    return this.root.getByTestId("preference-absent");
  }

  get unknownTypes(): Locator {
    return this.root.getByTestId("preference-unknown-types");
  }

  get deliversNothing(): Locator {
    return this.root.getByTestId("preference-delivers-nothing");
  }

  get saveFailed(): Locator {
    return this.root.getByTestId("preference-save-failed");
  }

  get saved(): Locator {
    return this.root.getByTestId("preference-saved");
  }

  get save(): Locator {
    return this.root.getByRole("button", { name: "Save preference" });
  }

  get allFutureTypes(): Locator {
    return this.root.getByLabel("Opt into every notification type added for this resource");
  }

  type(name: string): Locator {
    return this.root.getByRole("checkbox", { name, exact: true });
  }
}
