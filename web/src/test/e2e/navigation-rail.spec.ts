import { expect, test } from "./harness.js";
import { ConsoleShell } from "./pages/index.js";

/** The two-state navigation rail's geometry and density, asserted from the
 * rendered box model and computed styles -- there are no screenshot baselines.
 *
 * The rail is the last structural region of the console's reconstruction to the
 * Foundry platform-sidebar geometry: an expanded labeled rail (the shell's
 * `--console-rail` width, inside the rulebook's 230-260px band) that collapses
 * to a 48px icon rail. The claims below are the load-bearing ones a content
 * test cannot see: that collapse changes information *density* (labels and the
 * group heading gone, per-item tooltips restored), that the active route row
 * carries the primary-tinted fill and the 4px left indicator (the Control Panel
 * rule), and that the work column is fluid `minmax(0,1fr)` beside the fixed
 * rail. docs/ui/console-visual-language.md section 10; docs/ui/foundry rulebook
 * (Tenant observation). */

const QUIET = {
  generatedAt: "2026-08-09T12:00:00Z",
  awaitingApprovals: { status: "CURRENT", count: 0, items: [] },
  failedPlans: { status: "CURRENT", count: 0, items: [] },
  runningPlans: { status: "CURRENT", count: 0, items: [] },
  unhealthyEntities: { status: "CURRENT", count: 0, items: [] },
  staleReports: { status: "CURRENT", count: 0, items: [] },
  blockedPromotions: { status: "CURRENT", count: 0, items: [] },
};

function quiet(hub: { get: (suffix: string, body: unknown) => void }) {
  hub.get("/overview", QUIET);
}

test.describe("navigation rail geometry", () => {
  test("opens expanded, showing labeled rows and the Applications group heading", async ({
    page,
    hub,
  }) => {
    quiet(hub);
    await page.goto("overview");
    const shell = new ConsoleShell(page);

    // Expanded width: the labeled rail, inside the rulebook's 230-260px band.
    const railBox = await shell.globalNavigation.boundingBox();
    expect(railBox?.width).toBe(230);

    // Labels are visible text, not only accessible names.
    await expect(shell.globalNavigation.getByText("Home", { exact: true })).toBeVisible();
    await expect(shell.applicationsGroup.getByText("Applications", { exact: true })).toBeVisible();

    // Measured row heights: 32px core rows, 36px application rows; 17px inset,
    // 12px icon-to-label gap.
    const home = await shell.primaryLink("Home").boundingBox();
    const fleet = await shell.applicationLink("Fleet").boundingBox();
    expect(home?.height).toBe(32);
    expect(fleet?.height).toBe(36);
    const rowStyles = await shell.primaryLink("Home").evaluate((element) => {
      const style = getComputedStyle(element);
      return { paddingLeft: style.paddingLeft, gap: style.columnGap };
    });
    expect(rowStyles.paddingLeft).toBe("17px");
    expect(rowStyles.gap).toBe("12px");
  });

  test("seats the work column as a fluid minmax(0,1fr) beside the fixed rail", async ({
    page,
    hub,
  }) => {
    quiet(hub);
    await page.goto("overview");
    const shell = new ConsoleShell(page);

    const railBox = await shell.globalNavigation.boundingBox();
    const canvasBox = await shell.workspaceCanvas.boundingBox();
    expect(railBox).not.toBeNull();
    expect(canvasBox).not.toBeNull();
    if (railBox === null || canvasBox === null) return;

    // The canvas begins exactly where the fixed rail ends, and takes the rest of
    // the viewport -- the fixed-rail + fluid-work equation, not a split.
    expect(canvasBox.x).toBeCloseTo(railBox.x + railBox.width, 0);
    const viewport = page.viewportSize();
    expect(viewport).not.toBeNull();
    if (viewport !== null) {
      expect(canvasBox.width).toBeCloseTo(viewport.width - railBox.width, 0);
    }

    // The shell resolves to two tracks: the fixed rail first, then the fluid
    // work column. The rail track is 230px; the work track is much wider.
    const tracks = await page
      .locator(".console-shell")
      .evaluate((element) => getComputedStyle(element).gridTemplateColumns);
    const widths = tracks.split(" ").map((value) => Number.parseFloat(value));
    expect(widths).toHaveLength(2);
    const railTrack = widths[0] ?? 0;
    const workTrack = widths[1] ?? 0;
    expect(railTrack).toBe(230);
    expect(workTrack).toBeGreaterThan(railTrack * 1.4);
  });

  test("collapsing hides labels and the heading, and restores the tooltips", async ({
    page,
    hub,
  }) => {
    quiet(hub);
    await page.goto("overview");
    const shell = new ConsoleShell(page);

    await shell.collapseToggle.click();

    // Density, not just width: the rail is now 48px, the labels and the group
    // heading are gone, and the Applications section is its popover again.
    const railBox = await shell.globalNavigation.boundingBox();
    expect(railBox?.width).toBe(48);
    await expect(shell.globalNavigation.getByText("Home", { exact: true })).toHaveCount(0);
    await expect(shell.applicationsGroup).toHaveCount(0);
    await expect(shell.applicationsButton).toBeVisible();

    // The destination is still a link (named to a reader by its aria-label) and
    // is now named to a pointer by a tooltip, which the labeled rail did not
    // need.
    const home = shell.globalNavigation.getByRole("link", { name: "Home" });
    await expect(home).toBeVisible();
    await home.hover();
    await expect(page.locator(".bp6-tooltip").filter({ hasText: "Home" })).toBeVisible();

    // Expanding restores the labels.
    await shell.collapseToggle.click();
    await expect(shell.globalNavigation.getByText("Home", { exact: true })).toBeVisible();
  });

  test("marks the active route with the primary tint and a 4px left indicator", async ({
    page,
    hub,
  }) => {
    hub.get("/environments", {
      environments: [],
      total: 0,
      pageSize: 0,
      limit: 100,
      hasPrevious: false,
    });
    await page.goto("environments");
    const shell = new ConsoleShell(page);

    const active = shell.primaryLink("Environments");
    await expect(active).toHaveAttribute("aria-current", "page");

    const treatment = await active.evaluate((element) => {
      const fill = getComputedStyle(element).backgroundColor;
      const indicator = getComputedStyle(element, "::before");
      return { fill, indicatorWidth: indicator.width, indicatorColor: indicator.backgroundColor };
    });
    // Low-opacity primary fill, not the neutral hover fill.
    expect(treatment.fill).toBe("rgba(45, 114, 210, 0.2)");
    // The 4px left indicator, drawn in the solid primary.
    expect(treatment.indicatorWidth).toBe("4px");
    expect(treatment.indicatorColor).toBe("rgb(45, 114, 210)");

    // Hover is not selection: an inactive row carries neither the tint nor the
    // indicator.
    const inactive = shell.primaryLink("Home");
    await expect(inactive).not.toHaveAttribute("aria-current", "page");
    const inactiveIndicator = await inactive.evaluate(
      (element) => getComputedStyle(element, "::before").content,
    );
    expect(inactiveIndicator).toBe("none");
  });
});
