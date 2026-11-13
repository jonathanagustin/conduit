import test from "node:test";
import { chromium, expect } from "@playwright/test";

test(
  "Temporal rehearsal streams evidence, approves, resumes a pending workflow, and rejects failed validation",
  {
    skip: !process.env.CONDUIT_TEMPORAL_DEMO_URL,
    timeout: 60000,
  },
  async () => {
    const browser = await chromium.launch({ headless: true });
    try {
      const page = await browser.newPage();
      const errors = [];
      page.on("pageerror", (error) => errors.push(error.message));
      await page.goto(process.env.CONDUIT_TEMPORAL_DEMO_URL, {
        waitUntil: "networkidle",
      });
      const submit = async (text) => {
        await page.getByRole("textbox", { name: "Message input" }).fill(text);
        await page
          .getByRole("button", { name: "Send message", exact: true })
          .click();
      };
      await submit("Rehearse dependency remediation");
      await page
        .getByRole("button", { name: "Approve simulation", exact: true })
        .click();
      await expect(
        page.getByText("Temporal rehearsal: completed.", { exact: false }),
      ).toBeVisible();
      await submit("Keep this workflow pending across reconnect");
      await expect(
        page.getByRole("button", { name: "Deny simulation", exact: true }),
      ).toBeVisible();
      const workflowId = await page
        .locator("code")
        .filter({ hasText: /^conduit-rehearsal-/ })
        .last()
        .innerText();
      await page.reload({ waitUntil: "networkidle" });
      await submit(`resume ${workflowId}`);
      await page
        .getByRole("button", { name: "Deny simulation", exact: true })
        .click();
      await expect(
        page.getByText("Temporal rehearsal: denied.", { exact: false }),
      ).toBeVisible();
      await submit("fail-validation");
      await expect(
        page.getByText("Temporal rehearsal: failed.", { exact: false }),
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: "Approve simulation", exact: true }),
      ).toHaveCount(0);
      expect(errors).toEqual([]);
    } finally {
      await browser.close();
    }
  },
);
