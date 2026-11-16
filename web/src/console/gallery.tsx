"use client";

import { useState } from "react";

import {
  ActionButton,
  AuditRail,
  Checklist,
  DataTable,
  DisabledWithReason,
  EmptyState,
  ErrorState,
  FacetRail,
  FilterBar,
  GateStrip,
  HealthBar,
  MetricCard,
  PipelineGraph,
  PriorityTag,
  QuickFilterBand,
  RangeTimeline,
  Sparkline,
  StatusChip,
  Stepper,
  SummaryBand,
  Kanban,
  KpiStrip,
  NotePopover,
  Panel,
  PromptDialog,
  SideDrawer,
  StatCard,
  StatusTag,
  StepTimeline,
  Surface,
  TextField,
} from "./components/base";
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
} from "../components/index.js";
import "./theme/tokens.css";

const INTENTS = ["none", "primary", "success", "warning", "danger"] as const;
const EMPHASIS = ["minimal", "outlined", "filled"] as const;
const SIZES = ["sm", "md", "lg"] as const;

export function ComponentGallery() {
  const [theme, setTheme] = useState<"dark" | "light">("dark");
  const [drawer, setDrawer] = useState(false);
  const [dialog, setDialog] = useState(false);
  const [text, setText] = useState("lab");
  const [switchChecked, setSwitchChecked] = useState(true);
  return (
    <div
      className={theme === "dark" ? "bp6-dark conduit-shell" : "conduit-shell"}
      data-theme={theme}
    >
      <div className="conduit-row" style={{ padding: 12 }}>
        <strong>Component gallery</strong>
        <ActionButton
          emphasis="outlined"
          onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
        >
          {theme === "dark" ? "Light" : "Dark"}
        </ActionButton>
      </div>
      <section>
        <h2>StatusTag</h2>
        {EMPHASIS.map((emphasis) => (
          <div key={emphasis} className="conduit-row">
            {INTENTS.map((intent) => (
              <StatusTag key={intent} intent={intent} emphasis={emphasis}>
                {intent} {emphasis}
              </StatusTag>
            ))}
          </div>
        ))}
        <div className="conduit-row">
          <StatusTag gate="GO">GO</StatusTag>
          <StatusTag gate="CAUTION">CAUTION</StatusTag>
          <StatusTag gate="NO-GO">NO-GO</StatusTag>
        </div>
      </section>
      <section>
        <h2>StatCard</h2>
        <KpiStrip>
          {SIZES.map((size) => (
            <StatCard
              key={size}
              label={size}
              value="12"
              size={size}
              trend={size === "sm" ? "up" : size === "md" ? "flat" : "down"}
            />
          ))}
        </KpiStrip>
      </section>
      <section>
        <h2>Buttons and planned actions</h2>
        <div className="conduit-row">
          {EMPHASIS.map((emphasis) => (
            <ActionButton key={emphasis} emphasis={emphasis} intent="primary">
              {emphasis}
            </ActionButton>
          ))}
          <DisabledWithReason
            reason="Disabled until the binding is complete."
            label="Retry"
          />
        </div>
      </section>
      <section>
        <h2>Timeline, stepper, and gates</h2>
        <SummaryBand>
          <MetricCard
            label="Example"
            value="4"
            tone="ok"
            points={[1, 2, 3, 4]}
            detail="Example trend"
          />
        </SummaryBand>
        <Sparkline points={[1, 3, 2, 5]} />
        <GateStrip total={6} ok={3} warn={1} bad={1} size="lg" />
        <Stepper
          steps={[
            { id: "a", label: "Fetch", state: "done" },
            { id: "b", label: "Scan", state: "current" },
            { id: "c", label: "Sign", state: "waiting" },
          ]}
        />
        <RangeTimeline
          rows={[
            {
              id: "row",
              label: "Example",
              bars: [
                {
                  id: "bar",
                  label: "Example",
                  start: 0.1,
                  end: 0.5,
                  tone: "ok",
                },
              ],
            },
          ]}
        />
        <QuickFilterBand
          options={[
            { id: "all", label: "All" },
            { id: "open", label: "Open" },
          ]}
          selected="all"
          onSelect={() => undefined}
        />
        <FacetRail
          groups={[
            {
              id: "source",
              label: "Source",
              options: [{ id: "example", label: "Example", count: 1 }],
            },
          ]}
          selected={[]}
          onToggle={() => undefined}
        />
        <StatusChip label="recorded" intent="success" />
        <PriorityTag priority="high" />
      </section>
      <section>
        <h2>Health and channels</h2>
        <HealthBar ok={3} bad={1} other={1} />
        <PipelineGraph
          nodes={[
            { id: "one", label: "Candidate" },
            { id: "two", label: "Stable" },
          ]}
          selected="two"
        />
      </section>
      <section>
        <h2>DataTable density</h2>
        <DataTable
          density="comfortable"
          columns={["Name"]}
          rows={[["comfortable"]]}
        />
        <DataTable density="compact" columns={["Name"]} rows={[["compact"]]} />
      </section>
      <section>
        <h2>Panel elevation</h2>
        <Panel title="Minimal" emphasis="minimal">
          Minimal section
        </Panel>
        <Panel title="Outlined" emphasis="outlined">
          Outlined section
        </Panel>
        <Panel title="Filled" emphasis="filled">
          Filled section
        </Panel>
      </section>
      <section>
        <h2>Checklist states</h2>
        <Checklist
          items={[
            {
              id: "pass",
              label: "Passed",
              passed: true,
              detail: "Requirement met.",
            },
            {
              id: "fail",
              label: "Failed",
              passed: false,
              detail: "Requirement missing.",
            },
          ]}
        />
      </section>
      <section>
        <h2>States</h2>
        <Surface state="empty" reason="Nothing loaded." />
        <Surface state="error" reason="The Hub refused the read." />
        <Surface state="loading" />
        <EmptyState title="Empty" detail="No records." />
        <ErrorState title="Error" detail="Request failed." />
      </section>
      <section>
        <h2>Custom compositions</h2>
        <StepTimeline
          steps={[
            { id: "a", label: "Assess", mark: "done" },
            { id: "b", label: "Review", mark: "current" },
          ]}
        />
        <Kanban
          columns={[{ id: "one", title: "One" }]}
          cards={[{ id: "card", name: "Card" }]}
          columnOf={() => "one"}
          renderCard={(card) => <span>{card.name}</span>}
        />
        <AuditRail
          title="Audit"
          lines={[{ id: "1", text: "Phase review" }]}
          empty="None"
        />
        <FilterBar
          label="Status"
          value=""
          onChange={() => undefined}
          options={["", "GO"]}
        />
        <TextField label="Run" labelFor="run" value={text} onChange={setText} />
        <NotePopover
          label="Note"
          content="Tooltip content lives in a popover."
        />
        <ActionButton emphasis="minimal" onClick={() => setDrawer(true)}>
          Drawer
        </ActionButton>
        <ActionButton emphasis="minimal" onClick={() => setDialog(true)}>
          Dialog
        </ActionButton>
        <SideDrawer
          open={drawer}
          title="Detail"
          onClose={() => setDrawer(false)}
        >
          Drawer body
        </SideDrawer>
        <PromptDialog
          open={dialog}
          title="Confirm"
          onClose={() => setDialog(false)}
        >
          Dialog body
        </PromptDialog>
      </section>
      <section>
        <h2>Shared Conduit UI Kit Primitives</h2>
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          <ConduitEntityTitle
            title="EntityTitle primitive"
            subtitle="Scoped to Inspector panels and entity cards with zero-gutter alignment"
            tags={
              <>
                <OrbCountBadge count={42} intent="primary" />
                <KeyComboTag combo="⌘K" />
              </>
            }
          />
          <ConduitCardList bordered compact>
            <div style={{ padding: 12 }}>
              <ConduitSwitchCard
                label="Governed two-person sign-off"
                helperText="Requires explicit approval from governance authority before release handoff."
                checked={switchChecked}
                onChange={setSwitchChecked}
              />
            </div>
          </ConduitCardList>
          <div>
            <h3>PipelineStepper</h3>
            <PipelineStepper
              steps={[
                { id: "assess", label: "Assess", state: "done" },
                { id: "build", label: "Build", state: "done" },
                { id: "verify", label: "Verify", state: "active" },
                { id: "review", label: "Review", state: "waiting" },
                { id: "handoff", label: "Handoff", state: "waiting" },
              ]}
              ariaLabel="Gallery pipeline demo"
            />
          </div>
          <div>
            <h3>MetadataGrid & DigestPill</h3>
            <MetadataGrid
              items={[
                { label: "Target Coordinate", value: "spoke-fleet-east" },
                {
                  label: "Source Digest",
                  value: (
                    <DigestPill
                      value="sha256:7f83b1657ff1fc53b92dc18148a1d65dfc2d4b1fa3d677284addd200126d9069"
                      label="sha256"
                    />
                  ),
                  mono: true,
                },
                {
                  label: "Artifact Digest",
                  value: (
                    <DigestPill
                      value="sha256:8b0e8c14f6b6eb8b07f87c1f8f30730d10c2d3345842845c479e0a8ff2472d73"
                      label="sha256"
                    />
                  ),
                  mono: true,
                },
                { label: "Status", value: "Active", mono: false },
              ]}
            />
          </div>
          <div>
            <h3>ViewStateBoundary (Stale state with preserved children)</h3>
            <ViewStateBoundary
              state="stale"
              title="Telemetry sync pending"
              description="Spoke node reporting delayed by 45s. Live records preserved below."
            >
              <div
                style={{
                  padding: 12,
                  background: "var(--cnd-bg-1, #10161a)",
                  borderRadius: 4,
                  border: "1px solid var(--cnd-border-subtle, #1e2830)",
                }}
              >
                Preserved Hub recorded view content during staleness.
              </div>
            </ViewStateBoundary>
          </div>
        </div>
      </section>
    </div>
  );
}
