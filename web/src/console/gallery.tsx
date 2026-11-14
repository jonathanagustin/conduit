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
  FilterBar,
  HealthBar,
  PipelineGraph,
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
import "./theme/tokens.css";

const INTENTS = ["none", "primary", "success", "warning", "danger"] as const;
const EMPHASIS = ["minimal", "outlined", "filled"] as const;
const SIZES = ["sm", "md", "lg"] as const;

export function ComponentGallery() {
  const [theme, setTheme] = useState<"dark" | "light">("dark");
  const [drawer, setDrawer] = useState(false);
  const [dialog, setDialog] = useState(false);
  const [text, setText] = useState("lab");
  return (
    <div className={theme === "dark" ? "bp6-dark conduit-shell" : "conduit-shell"} data-theme={theme}>
      <div className="conduit-row" style={{ padding: 12 }}>
        <strong>Component gallery</strong>
        <ActionButton emphasis="outlined" onClick={() => setTheme(theme === "dark" ? "light" : "dark")}>
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
            <StatCard key={size} label={size} value="12" size={size} trend={size === "sm" ? "up" : size === "md" ? "flat" : "down"} />
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
          <DisabledWithReason reason="Disabled until the binding is complete." label="Retry" />
        </div>
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
        <DataTable density="comfortable" columns={["Name"]} rows={[["comfortable"]]} />
        <DataTable density="compact" columns={["Name"]} rows={[["compact"]]} />
      </section>
      <section>
        <h2>Panel elevation</h2>
        <Panel title="Minimal" emphasis="minimal">Minimal section</Panel>
        <Panel title="Outlined" emphasis="outlined">Outlined section</Panel>
        <Panel title="Filled" emphasis="filled">Filled section</Panel>
      </section>
      <section>
        <h2>Checklist states</h2>
        <Checklist
          items={[
            { id: "pass", label: "Passed", passed: true, detail: "Requirement met." },
            { id: "fail", label: "Failed", passed: false, detail: "Requirement missing." },
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
        <StepTimeline steps={[{ id: "a", label: "Assess", mark: "done" }, { id: "b", label: "Review", mark: "current" }]} />
        <Kanban
          columns={[{ id: "one", title: "One" }]}
          cards={[{ id: "card", name: "Card" }]}
          columnOf={() => "one"}
          renderCard={(card) => <span>{card.name}</span>}
        />
        <AuditRail title="Audit" lines={[{ id: "1", text: "Phase review" }]} empty="None" />
        <FilterBar label="Status" value="" onChange={() => undefined} options={["", "GO"]} />
        <TextField label="Run" labelFor="run" value={text} onChange={setText} />
        <NotePopover label="Note" content="Tooltip content lives in a popover." />
        <ActionButton emphasis="minimal" onClick={() => setDrawer(true)}>Drawer</ActionButton>
        <ActionButton emphasis="minimal" onClick={() => setDialog(true)}>Dialog</ActionButton>
        <SideDrawer open={drawer} title="Detail" onClose={() => setDrawer(false)}>Drawer body</SideDrawer>
        <PromptDialog open={dialog} title="Confirm" onClose={() => setDialog(false)}>Dialog body</PromptDialog>
      </section>
    </div>
  );
}
