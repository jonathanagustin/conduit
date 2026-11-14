import {
  Button,
  Callout,
  Card,
  Dialog,
  Drawer,
  EntityTitle,
  FormGroup,
  HTMLSelect,
  HTMLTable,
  Icon,
  InputGroup,
  Menu,
  MenuItem,
  NonIdealState,
  Popover,
  ProgressBar,
  Section,
  SectionCard,
  Spinner,
  Tabs,
  Tab,
  Tag,
  Tooltip,
  Tree,
} from "@blueprintjs/core";
import type { TreeNodeInfo } from "@blueprintjs/core";
import { useState, type ReactNode } from "react";

import {
  blueprintVariant,
  ELEVATION,
  GATE_PRESET,
  TREND_ICON,
  type DensityName,
  type EmphasisName,
  type GateName,
  type IntentName,
  type SizeName,
  type SurfaceState,
  type TrendName,
  type VariantProps,
} from "./variants";

import "@blueprintjs/core/lib/css/blueprint.css";

function tagSize(size: SizeName | undefined): "medium" | "large" {
  return size === "lg" ? "large" : "medium";
}

export function StatusTag({
  intent = "none",
  size = "md",
  emphasis = "filled",
  gate,
  children,
}: VariantProps & { gate?: GateName; children: ReactNode }) {
  const preset = gate ? GATE_PRESET[gate] : {};
  const mapped = blueprintVariant({ intent, size, emphasis, ...preset });
  return (
    <Tag
      round
      intent={mapped.intent}
      minimal={mapped.minimal}
      size={tagSize(size)}
    >
      {gate ? <Icon icon={gate === "GO" ? "tick" : gate === "NO-GO" ? "cross" : "warning-sign"} size={12} /> : null}{" "}
      {children}
    </Tag>
  );
}

export function ActionButton({
  intent = "none",
  size = "md",
  emphasis = "filled",
  disabled,
  loading,
  children,
  onClick,
}: VariantProps & {
  disabled?: boolean;
  loading?: boolean;
  children: ReactNode;
  onClick?: () => void;
}) {
  const mapped = blueprintVariant({ intent, size, emphasis });
  return (
    <Button
      intent={mapped.intent}
      size={mapped.size}
      minimal={emphasis === "minimal"}
      outlined={emphasis === "outlined"}
      disabled={disabled}
      loading={loading}
      onClick={onClick}
    >
      {children}
    </Button>
  );
}

export function DisabledWithReason({ reason, label }: { reason: string; label: string }) {
  return (
    <Tooltip content={reason} disabled={false}>
      <span>
        <Button disabled>{label}</Button>
      </span>
    </Tooltip>
  );
}

export function StatCard({
  label,
  value,
  detail,
  size = "md",
  trend,
}: {
  label: string;
  value: string;
  detail?: string;
  size?: SizeName;
  trend?: TrendName;
}) {
  const mapped = blueprintVariant({ size });
  return (
    <Card compact={mapped.compact || size === "sm"} elevation={size === "lg" ? 2 : 1}>
      <EntityTitle title={label} subtitle={detail} />
      <div className="conduit-row">
        <strong>{value}</strong>
        {trend ? (
          <Tag
            minimal
            {...(trend === "flat" ? {} : { intent: trend === "down" ? ("danger" as const) : ("success" as const) })}
            icon={TREND_ICON[trend]}
          >
            {trend}
          </Tag>
        ) : null}
      </div>
    </Card>
  );
}

export function KpiStrip({ children }: { children: ReactNode }) {
  return <div className="conduit-kpi">{children}</div>;
}

export function Panel({
  title,
  emphasis = "filled",
  children,
}: {
  title?: string;
  emphasis?: EmphasisName;
  children: ReactNode;
}) {
  return (
    <Section title={title} elevation={ELEVATION[emphasis]} compact={emphasis === "minimal"}>
      <SectionCard>{children}</SectionCard>
    </Section>
  );
}

export function PageHeader({ title, detail }: { title: string; detail?: string }) {
  return <EntityTitle title={title} subtitle={detail} />;
}

export function ObjectHeader({
  title,
  eyebrow,
  tags,
}: {
  title: string;
  eyebrow?: string;
  tags?: ReactNode;
}) {
  return (
    <Card elevation={1}>
      <EntityTitle title={title} subtitle={eyebrow} icon="application" />
      {tags}
    </Card>
  );
}

export function PropertyCard({ label, value }: { label: string; value: string }) {
  return (
    <Card compact>
      <EntityTitle title={value || "—"} subtitle={label} />
    </Card>
  );
}

export function EntityCard({
  title,
  meta,
  children,
}: {
  title: string;
  meta?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <Card elevation={1}>
      <div className="conduit-row">
        <EntityTitle title={title} />
        {meta}
      </div>
      {children}
    </Card>
  );
}

export function SummaryBar({ items }: { items: readonly { label: string; value: string }[] }) {
  return (
    <KpiStrip>
      {items.map((item) => (
        <StatCard key={item.label} label={item.label} value={item.value} size="sm" />
      ))}
    </KpiStrip>
  );
}

export function TabBar({
  tabs,
  current,
  onSelect,
}: {
  tabs: readonly { id: string; label: string }[];
  current: string;
  onSelect: (id: string) => void;
}) {
  return (
    <Tabs id="conduit-tabs" selectedTabId={current} onChange={(id) => onSelect(String(id))}>
      {tabs.map((tab) => (
        <Tab key={tab.id} id={tab.id} title={tab.label} />
      ))}
    </Tabs>
  );
}

export function FilterBar({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: readonly string[];
}) {
  return (
    <FormGroup label={label}>
      <HTMLSelect value={value} onChange={(event) => onChange(event.currentTarget.value)}>
        {options.map((option) => (
          <option key={option || "all"} value={option}>
            {option || "All"}
          </option>
        ))}
      </HTMLSelect>
    </FormGroup>
  );
}

export function DataTable({
  columns,
  rows,
  density = "comfortable",
}: {
  columns: readonly string[];
  rows: readonly (readonly ReactNode[])[];
  density?: DensityName;
}) {
  return (
    <div className="conduit-table-scroll">
    <HTMLTable compact={density === "compact"} striped>
      <thead>
        <tr>
          {columns.map((column) => (
            <th key={column}>{column}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, index) => (
          <tr key={index}>
            {row.map((cell, cellIndex) => (
              <td key={cellIndex}>{cell}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </HTMLTable>
    </div>
  );
}

export function DenseTable({
  columns,
  rows,
  selected,
  onSelect,
  density = "compact",
}: {
  columns: readonly string[];
  rows: readonly { id: string; cells: readonly ReactNode[] }[];
  selected?: string;
  onSelect?: (id: string) => void;
  density?: DensityName;
}) {
  return (
    <div className="conduit-table-scroll">
    <HTMLTable compact={density === "compact"} interactive striped>
      <thead>
        <tr>
          {columns.map((column) => (
            <th key={column}>{column}</th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => (
          <tr key={row.id} aria-selected={row.id === selected} onClick={() => onSelect?.(row.id)}>
            {row.cells.map((cell, index) => (
              <td key={index}>{cell}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </HTMLTable>
    </div>
  );
}

export function SidebarNav({
  items,
  current,
  onSelect,
}: {
  items: readonly { id: string; label: string }[];
  current: string;
  onSelect: (id: string) => void;
}) {
  return (
    <Menu aria-label="Workbench">
      {items.map((item) => (
        <MenuItem
          key={item.id}
          text={item.label}
          active={item.id === current}
          onClick={() => onSelect(item.id)}
        />
      ))}
    </Menu>
  );
}

export function NavTree({
  nodes,
  onSelect,
}: {
  nodes: readonly { id: string; label: string }[];
  onSelect: (id: string) => void;
}) {
  const tree: TreeNodeInfo[] = nodes.map((node) => ({
    id: node.id,
    label: node.label,
  }));
  return <Tree contents={tree} onNodeClick={(node) => onSelect(String(node.id))} />;
}

export function AppShell({
  kpi,
  toolbar,
  sidebar,
  tabs,
  banner,
  footer,
  children,
  rail,
}: {
  kpi: ReactNode;
  toolbar?: ReactNode;
  sidebar: ReactNode;
  tabs?: ReactNode;
  banner?: ReactNode;
  footer?: ReactNode;
  children: ReactNode;
  rail: ReactNode;
}) {
  const [navOpen, setNavOpen] = useState(false);
  const [railCollapsed, setRailCollapsed] = useState(false);
  const [chatOpen, setChatOpen] = useState(false);
  const shellClass = [
    "dark",
    "bp6-dark",
    "conduit-shell",
    railCollapsed ? "is-rail-collapsed" : "",
    navOpen ? "is-nav-open" : "",
    chatOpen ? "is-chat-open" : "",
  ]
    .filter((name) => name.length > 0)
    .join(" ");
  return (
    <div className={shellClass} data-theme="dark">
      {banner}
      <header className="cd-topbar">
        <Button
          className="conduit-nav-toggle"
          icon="menu"
          minimal
          aria-label="Toggle navigation"
          onClick={() => setNavOpen((open) => !open)}
        />
        <Button
          className="conduit-rail-collapse"
          icon="chevron-left"
          minimal
          aria-label="Collapse sidebar"
          onClick={() => setRailCollapsed((collapsed) => !collapsed)}
        />
        <span className="cd-wordmark">Conduit</span>
        <div className="cd-kpi-strip">{kpi}</div>
        <div className="conduit-top-tools">{toolbar}</div>
        <Button
          className="conduit-chat-toggle"
          icon="chat"
          minimal
          aria-label="Toggle assistant"
          onClick={() => setChatOpen((open) => !open)}
        />
      </header>
      <button
        type="button"
        className="conduit-nav-backdrop"
        aria-label="Close overlays"
        onClick={() => {
          setNavOpen(false);
          setChatOpen(false);
        }}
      />
      <div className="conduit-body">
        <aside className="conduit-side" onClick={() => setNavOpen(false)}>
          {sidebar}
        </aside>
        <section className="conduit-main">
          <div className="conduit-tabstrip">{tabs}</div>
          <div className="conduit-center">{children}</div>
        </section>
        <aside className="conduit-chat" aria-label="Assistant">
          {rail}
        </aside>
      </div>
      {footer}
    </div>
  );
}

export function SplitListDetail({ list, detail }: { list: ReactNode; detail: ReactNode }) {
  return (
    <div className="conduit-split">
      <div>{list}</div>
      <div>{detail}</div>
    </div>
  );
}

export function SplitView({ list, detail }: { list: ReactNode; detail: ReactNode }) {
  return <SplitListDetail list={list} detail={detail} />;
}

export function Checklist({
  items,
}: {
  items: readonly { id: string; label: string; passed: boolean; detail: string }[];
}) {
  return (
    <ul>
      {items.map((item) => (
        <li key={item.id}>
          <StatusTag gate={item.passed ? "GO" : "NO-GO"} emphasis={item.passed ? "filled" : "outlined"}>
            {item.passed ? "pass" : "fail"}
          </StatusTag>{" "}
          {item.label}
          <div className="conduit-muted">{item.detail}</div>
        </li>
      ))}
    </ul>
  );
}

export function AuditRail({
  title,
  lines,
  empty,
}: {
  title: string;
  lines: readonly { id: string; text: string }[];
  empty: string;
}) {
  const ordered = [...lines].reverse();
  return (
    <Card>
      <EntityTitle title={title} icon="history" />
      {ordered.length === 0 ? <NonIdealState title={empty} icon="inbox" /> : null}
      <Menu>
        {ordered.map((line) => (
          <MenuItem key={line.id} text={line.text} />
        ))}
      </Menu>
    </Card>
  );
}

export function ActivityTimeline({ items }: { items: readonly { id: string; text: string }[] }) {
  return (
    <Menu>
      {[...items].reverse().map((item) => (
        <MenuItem key={item.id} text={item.text} icon="time" />
      ))}
    </Menu>
  );
}

export function Kanban<T extends { id: string }>({
  columns,
  cards,
  columnOf,
  renderCard,
}: {
  columns: readonly { id: string; title: string }[];
  cards: readonly T[];
  columnOf: (card: T) => string;
  renderCard: (card: T) => ReactNode;
}) {
  return (
    <div className="conduit-kanban">
      {columns.map((column) => (
        <Card key={column.id} elevation={0}>
          <EntityTitle title={column.title} />
          {cards.filter((card) => columnOf(card) === column.id).map((card) => (
            <div key={card.id}>{renderCard(card)}</div>
          ))}
        </Card>
      ))}
    </div>
  );
}

export function EmptyState({ title, detail }: { title: string; detail: string }) {
  return <NonIdealState title={title} description={detail} icon="inbox" />;
}

export function ErrorState({ title, detail }: { title: string; detail: string }) {
  return <Callout intent="danger" title={title} icon="error">{detail}</Callout>;
}

export function LoadingState({ label }: { label: string }) {
  return <Spinner size={20} aria-label={label} />;
}

export function Surface({
  state,
  reason,
  children,
}: {
  state?: SurfaceState;
  reason?: string;
  children?: ReactNode;
}) {
  if (state === "loading") return <LoadingState label="Loading" />;
  if (state === "empty") return <EmptyState title="Nothing here" detail={reason ?? "No records."} />;
  if (state === "error") return <ErrorState title="Something failed" detail={reason ?? "The request failed."} />;
  if (state === "disabled-with-reason") return <DisabledWithReason reason={reason ?? "Unavailable"} label="Unavailable" />;
  return <>{children}</>;
}

export function InboxList({
  items,
  selected,
  onSelect,
}: {
  items: readonly { id: string; title: string; detail: string }[];
  selected?: string;
  onSelect: (id: string) => void;
}) {
  return (
    <Menu>
      {items.map((item) => (
        <MenuItem
          key={item.id}
          active={item.id === selected}
          text={item.title}
          label={item.detail}
          onClick={() => onSelect(item.id)}
        />
      ))}
    </Menu>
  );
}

export function ReviewChecklist({
  items,
}: {
  items: readonly { id: string; label: string; passed: boolean; detail: string }[];
}) {
  return <Checklist items={items} />;
}

export function ReviewHistory({ items }: { items: readonly { id: string; text: string }[] }) {
  return <ActivityTimeline items={items} />;
}

export function DiffPane({ before, after }: { before: string; after: string }) {
  return (
    <div className="conduit-split">
      <PropertyCard label="Before" value={before || "—"} />
      <PropertyCard label="After" value={after || "—"} />
    </div>
  );
}

export function ConstraintRow({ label, reason, passed }: { label: string; reason: string; passed: boolean }) {
  return (
    <Callout intent={passed ? "success" : "danger"} icon={passed ? "tick" : "cross"}>
      <strong>{label}</strong> {reason}
    </Callout>
  );
}

export function RequirementChecklist({
  items,
}: {
  items: readonly { id: string; label: string; passed: boolean; detail: string }[];
}) {
  return <Checklist items={items} />;
}

export function ImpactSummary({ lines }: { lines: readonly string[] }) {
  return (
    <Card>
      <EntityTitle title="Impact" icon="changes" />
      {lines.length === 0 ? <NonIdealState title="No impact fields" icon="info-sign" /> : (
        <Menu>
          {lines.map((line) => (
            <MenuItem key={line} text={line} />
          ))}
        </Menu>
      )}
    </Card>
  );
}

export function StepTimeline({
  steps,
}: {
  steps: readonly { id: string; label: string; mark: string }[];
}) {
  const done = steps.filter((step) => step.mark === "done").length;
  return (
    <Card>
      <ProgressBar value={steps.length === 0 ? 0 : done / steps.length} />
      <div className="conduit-row">
        {steps.map((step) => (
          <Tag key={step.id} minimal={step.mark !== "current"} intent={step.mark === "done" ? "success" : step.mark === "current" ? "primary" : "none"}>
            {step.label}
          </Tag>
        ))}
      </div>
    </Card>
  );
}

export function TaskList({
  tasks,
}: {
  tasks: readonly { id: string; label: string; status: string }[];
}) {
  if (tasks.length === 0) return <NonIdealState title="No tasks reported" icon="list" />;
  return (
    <Menu>
      {tasks.map((task) => (
        <MenuItem key={task.id} text={task.label} label={task.status} />
      ))}
    </Menu>
  );
}

export function OutputPane({ text }: { text: string }) {
  const body = text || "No output on this route.";
  return (
    <Callout icon="console">
      <Button
        minimal
        icon="clipboard"
        aria-label="Copy output"
        onClick={() => {
          void navigator.clipboard?.writeText(body);
        }}
      >
        Copy
      </Button>
      <pre>{body}</pre>
    </Callout>
  );
}

export function HealthBar({ ok, bad, other }: { ok: number; bad: number; other: number }) {
  const total = Math.max(1, ok + bad + other);
  return (
    <div className="conduit-health" aria-label={`${ok} healthy, ${bad} unhealthy, ${other} other`}>
      <span data-tone="ok" style={{ width: `${(ok / total) * 100}%` }} />
      <span data-tone="bad" style={{ width: `${(bad / total) * 100}%` }} />
      <span data-tone="muted" style={{ width: `${(other / total) * 100}%` }} />
    </div>
  );
}

export function PipelineGraph({
  nodes,
  selected,
  onSelect,
}: {
  nodes: readonly { id: string; label: string }[];
  selected?: string;
  onSelect?: (id: string) => void;
}) {
  if (nodes.length === 0) return <NonIdealState title="No channels" icon="path-search" />;
  return (
    <div className="conduit-pipeline">
      {nodes.map((node, index) => (
        <span key={node.id} className="conduit-row">
          {index > 0 ? <Icon icon="arrow-right" /> : null}
          <Button
            minimal={node.id !== selected}
            intent={node.id === selected ? "primary" : "none"}
            onClick={() => onSelect?.(node.id)}
          >
            {node.label}
          </Button>
        </span>
      ))}
    </div>
  );
}

export function NotePopover({ label, content }: { label: string; content: string }) {
  return (
    <Popover content={<Card>{content}</Card>}>
      <Button minimal icon="info-sign">{label}</Button>
    </Popover>
  );
}

export function SideDrawer({
  open,
  title,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <Drawer isOpen={open} title={title} onClose={onClose}>
      {children}
    </Drawer>
  );
}

export function PromptDialog({
  open,
  title,
  onClose,
  children,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <Dialog isOpen={open} title={title} onClose={onClose}>
      {children}
    </Dialog>
  );
}

export function TextField({
  label,
  value,
  onChange,
  labelFor,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  labelFor: string;
}) {
  return (
    <FormGroup label={label} labelFor={labelFor}>
      <InputGroup id={labelFor} value={value} onChange={(event) => onChange(event.currentTarget.value)} />
    </FormGroup>
  );
}

export type { IntentName };
export {
  ClassificationBanner,
  FacetRail,
  GateStrip,
  GraphView,
  KeyValue,
  MetricCard,
  PriorityTag,
  QuickFilterBand,
  RangeTimeline,
  SegmentedTabs,
  Sparkline,
  StatusChip,
  StatusTabs,
  Stepper,
  SummaryBand,
  TargetBoard,
} from "./patterns";
