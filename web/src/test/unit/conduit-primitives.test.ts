import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vite-plus/test";

import {
  ConduitCardList,
  ConduitEntityTitle,
  ConduitSwitchCard,
  DigestPill,
  KeyComboTag,
  MetadataGrid,
  OrbCountBadge,
  PipelineStepper,
  ViewStateBoundary,
  type ViewStateKind,
} from "../../components/index.js";

describe("Conduit React Primitives (Layer 2)", () => {
  describe("ConduitEntityTitle", () => {
    it("renders title, subtitle, and tags cleanly without phantom icon gutters", () => {
      const html = renderToStaticMarkup(
        createElement(ConduitEntityTitle, {
          title: "Intake Run INT-412",
          subtitle: "Target: Spoke cluster alpha",
          tags: createElement("span", { className: "test-tag" }, "Active"),
        }),
      );
      expect(html).toContain("Intake Run INT-412");
      expect(html).toContain("Target: Spoke cluster alpha");
      expect(html).toContain("test-tag");
      expect(html).not.toContain("bp6-entity-title-icon");
    });

    it("renders custom heading and actions wrapper when provided", () => {
      const html = renderToStaticMarkup(
        createElement(ConduitEntityTitle, {
          title: "Custom Heading Title",
          heading: ({ children, className }) =>
            createElement("h2", { className }, children),
          actions: createElement("button", { type: "button" }, "Action Button"),
        }),
      );
      expect(html).toContain("<h2");
      expect(html).toContain("Custom Heading Title");
      expect(html).toContain("cnd-entity-header");
      expect(html).toContain("Action Button");
    });

    it("applies fill and wrap classes", () => {
      const html = renderToStaticMarkup(
        createElement(ConduitEntityTitle, {
          title: "Wrapping Title",
          fill: true,
          wrap: true,
        }),
      );
      expect(html).toContain("cnd-entity-title-fill");
      expect(html).toContain("cnd-entity-title-wrap");
    });
  });

  describe("OrbCountBadge", () => {
    it("renders numeric counts correctly", () => {
      const html = renderToStaticMarkup(
        createElement(OrbCountBadge, { count: 7, intent: "primary" }),
      );
      expect(html).toContain("cnd-orb-badge");
      expect(html).toContain("bp6-round");
      expect(html).toContain("7");
    });

    it("caps count display at max prop", () => {
      const html = renderToStaticMarkup(
        createElement(OrbCountBadge, { count: 120, max: 99 }),
      );
      expect(html).toContain("99+");
    });
  });

  describe("KeyComboTag", () => {
    it("renders keyboard shortcut combo tag", () => {
      const html = renderToStaticMarkup(
        createElement(KeyComboTag, { combo: "⌘K" }),
      );
      expect(html).toContain("bp6-tag");
      expect(html).toContain("bp6-minimal");
      expect(html).toContain("⌘K");
    });
  });

  describe("ConduitCardList & ConduitSwitchCard", () => {
    it("renders compact bordered card list", () => {
      const html = renderToStaticMarkup(
        createElement(
          ConduitCardList,
          { compact: true, bordered: true, ariaLabel: "Settings list" },
          createElement("div", null, "Item 1"),
        ),
      );
      expect(html).toContain("bp6-card-list");
      expect(html).toContain("bp6-card-list-bordered");
      expect(html).toContain("bp6-compact");
      expect(html).toContain('aria-label="Settings list"');
      expect(html).toContain("Item 1");
    });

    it("renders switch card with label, helper, and toggle state", () => {
      const html = renderToStaticMarkup(
        createElement(ConduitSwitchCard, {
          label: "Require approval",
          helperText: "Requires two signatures before handoff",
          checked: true,
          onChange: () => undefined,
        }),
      );
      expect(html).toContain("cnd-switch-card");
      expect(html).toContain("Require approval");
      expect(html).toContain("Requires two signatures before handoff");
      expect(html).toContain("bp6-switch");
    });
  });

  describe("PipelineStepper", () => {
    it("renders pipeline stages with states and connector indicators", () => {
      const html = renderToStaticMarkup(
        createElement(PipelineStepper, {
          steps: [
            { id: "s1", label: "Intake", state: "done" },
            { id: "s2", label: "Verify", state: "active" },
            {
              id: "s3",
              label: "Review",
              state: "failed",
              detail: "Signature missing",
            },
            { id: "s4", label: "Deploy", state: "blocked" },
            { id: "s5", label: "Handoff", state: "waiting" },
          ],
          ariaLabel: "Packaging stages",
        }),
      );
      expect(html).toContain("cnd-pipeline-stepper");
      expect(html).toContain('aria-label="Packaging stages"');
      expect(html).toContain("is-done");
      expect(html).toContain("is-active");
      expect(html).toContain("is-failed");
      expect(html).toContain("is-blocked");
      expect(html).toContain("is-waiting");
      expect(html).toContain("✓");
      expect(html).toContain("✕");
      expect(html).toContain("!");
      expect(html).toContain("Signature missing");
      expect(html).toContain("cnd-pipeline-connector");
    });
  });

  describe("DigestPill & MetadataGrid", () => {
    it("renders monospace tabular-nums digest pill", () => {
      const html = renderToStaticMarkup(
        createElement(DigestPill, {
          value: "sha256:7f83b1657ff1",
          label: "artifact",
        }),
      );
      expect(html).toContain("cnd-digest-pill");
      expect(html).toContain("cnd-digest-pill-label");
      expect(html).toContain("artifact");
      expect(html).toContain("sha256:7f83b1657ff1");
    });

    it("renders semantic definition list metadata grid", () => {
      const html = renderToStaticMarkup(
        createElement(MetadataGrid, {
          items: [
            { label: "Target", value: "spoke-1" },
            { label: "Revision", value: "rev-4", mono: true },
          ],
        }),
      );
      expect(html).toContain("<dl");
      expect(html).toContain("cnd-meta-grid");
      expect(html).toContain("cnd-meta-card");
      expect(html).toContain("<dt");
      expect(html).toContain("Target");
      expect(html).toContain("<dd");
      expect(html).toContain("spoke-1");
      expect(html).toContain("is-mono");
    });
  });

  describe("ViewStateBoundary (7 states)", () => {
    const states: ViewStateKind[] = [
      "loading",
      "ready",
      "empty",
      "stale",
      "partial-failure",
      "permission-denied",
      "disconnected",
    ];

    it.each(states)("renders valid markup for %s state", (state) => {
      const html = renderToStaticMarkup(
        createElement(
          ViewStateBoundary,
          {
            state,
            testId: `boundary-${state}`,
            loadingLabel: "Custom loading…",
            title: `Title for ${state}`,
            description: `Description for ${state}`,
            onRetry: () => undefined,
          },
          createElement(
            "div",
            { className: "live-content" },
            "Live children content",
          ),
        ),
      );

      if (state === "ready") {
        expect(html).toContain("Live children content");
        expect(html).not.toContain("boundary-ready");
      } else if (state === "loading") {
        expect(html).toContain('data-testid="boundary-loading"');
        expect(html).toContain('role="status"');
        expect(html).toContain('aria-busy="true"');
        expect(html).toContain("Custom loading…");
        expect(html).toContain("cnd-skeleton-bar");
      } else if (state === "empty") {
        expect(html).toContain('data-testid="boundary-empty"');
        expect(html).toContain('role="status"');
        expect(html).toContain("Title for empty");
        expect(html).toContain("Description for empty");
      } else if (state === "permission-denied") {
        expect(html).toContain('data-testid="boundary-permission-denied"');
        expect(html).toContain('role="alert"');
        expect(html).toContain("Title for permission-denied");
      } else if (state === "disconnected") {
        expect(html).toContain('data-testid="boundary-disconnected"');
        expect(html).toContain('role="alert"');
        expect(html).toContain("Title for disconnected");
      } else if (state === "stale") {
        expect(html).toContain('data-testid="boundary-stale"');
        expect(html).toContain("Title for stale");
        expect(html).toContain("Description for stale");
        expect(html).toContain("Live children content");
      } else if (state === "partial-failure") {
        expect(html).toContain('data-testid="boundary-partial-failure"');
        expect(html).toContain('role="alert"');
        expect(html).toContain("Title for partial-failure");
        expect(html).toContain("Live children content");
      }
    });
  });
});
