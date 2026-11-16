import { mkdirSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";

import { test as base, type Page } from "@playwright/test";

const eventsFile = process.env.DEMO_EVENTS || "demo/out/events.json";
type Mark = { kind: string; at: number; value: string };
const events: Mark[] = [];
let startedAt = Date.now();

export function start() {
  startedAt = Date.now();
  events.push({ kind: "start", at: 0, value: String(startedAt / 1000) });
}

export function caption(text: string) {
  events.push({ kind: "caption", at: (Date.now() - startedAt) / 1000, value: text });
}

export async function still(page: Page, name: string) {
  const dir = process.env.DEMO_SHOTS || "demo/out/shots";
  mkdirSync(dir, { recursive: true });
  await page.screenshot({ path: `${dir}/${name}.png` });
}

function save() {
  mkdirSync(dirname(eventsFile), { recursive: true });
  writeFileSync(eventsFile, JSON.stringify(events, null, 2));
}

export const test = base.extend({
  page: async ({ page }, use) => {
    await page.setExtraHTTPHeaders({ "x-platform-subject": "alice" });
    await use(page);
    save();
  },
});

export { expect } from "@playwright/test";
