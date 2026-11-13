import test from "node:test";
import { chromium, expect } from "@playwright/test";

test(
  "simulation renders streamed results and handles approval, denial, and cancel",
  {
    skip: !process.env.CONDUIT_DEMO_URL,
    timeout: 30000,
  },
  async () => {
    const browser = await chromium.launch({ headless: true });
    try {
      const page = await browser.newPage();
      const errors = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.goto(process.env.CONDUIT_DEMO_URL, {
        waitUntil: "networkidle",
      });
      await expect(
        page.getByText("Simulation: fixture data only.", { exact: false }),
      ).toBeVisible();
      const input = page.getByRole("textbox", { name: "Message input" });
      const send = page.getByRole("button", {
        name: "Send message",
        exact: true,
      });
      await input.fill("Simulate a dependency update");
      await send.click();
      await page
        .getByRole("button", { name: "Approve simulation", exact: true })
        .click();
      await expect(
        page.getByText("Simulation approved. No application was deployed.", {
          exact: false,
        }),
      ).toBeVisible();
      await input.fill("Try again and deny");
      await send.click();
      await page
        .getByRole("button", { name: "Deny simulation", exact: true })
        .click();
      await expect(
        page.getByText("Simulation denied. Nothing was deployed.", {
          exact: false,
        }),
      ).toBeVisible();
      await input.fill("Cancel this simulation");
      await send.click();
      await page
        .getByRole("button", { name: "Stop generating", exact: true })
        .click();
      await expect(send).toBeVisible();
      expect(errors).toEqual([]);
    } finally {
      await browser.close();
    }
  },
);
