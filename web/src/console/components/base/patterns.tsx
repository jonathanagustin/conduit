import { Button, Card, EntityTitle, Tag, Tooltip } from "@blueprintjs/core";
import type { ReactNode } from "react";

import type { IntentName } from "./variants";

export function Sparkline({
  points,
  tone = "info",
}: {
  points: readonly number[];
  tone?: "ok" | "info" | "warn" | "bad";
}) {
  const width = 72;
  const height = 22;
  if (points.length < 2) return null;
  const min = Math.min(...points);
  const max = Math.max(...points);
  const span = Math.max(1, max - min);
  const coords = points
    .map((value, index) => {
      const x = (index / (points.length - 1)) * width;
      const y = height - 2 - ((value - min) / span) * (height - 4);
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");
  const color =
    tone === "ok"
      ? "var(--ok-text)"
      : tone === "warn"
        ? "var(--warn-text)"
        : tone === "bad"
          ? "var(--err-text)"
          : "var(--info-text)";
  return (
    <svg
      className="cd-spark"
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      aria-hidden="true"
    >
      <polyline points={coords} fill="none" stroke={color} strokeWidth="1.6" />
    </svg>
  );
}

export function MetricCard({
  label,
  value,
  detail,
  tone = "info",
  points,
}: {
  label: string;
  value: string;
  detail?: string;
  tone?: "ok" | "info" | "warn" | "bad";
  points?: readonly number[];
}) {
  return (
    <Card compact className="cd-panel">
      <EntityTitle title={value} subtitle={label} />
      {detail ? <div className="conduit-muted">{detail}</div> : null}
      {points ? <Sparkline points={points} tone={tone} /> : null}
    </Card>
  );
}

export function SummaryBand({ children }: { children: ReactNode }) {
  return <div className="cd-summary">{children}</div>;
}

export function GateStrip({
  total = 6,
  ok = 0,
  warn = 0,
  bad = 0,
  size = "sm",
}: {
  total?: number;
  ok?: number;
  warn?: number;
  bad?: number;
  size?: "sm" | "lg";
}) {
  const cells = Array.from({ length: total }, (_, index) => {
    const mark =
      index < ok
        ? "ok"
        : index < ok + warn
          ? "warn"
          : index < ok + warn + bad
            ? "bad"
            : "pending";
    return <span key={index} className="cd-gatecell" data-mark={mark} />;
  });
  return (
    <span className="cd-gatestrip" data-size={size} title={`${ok} of ${total}`}>
      {cells}
    </span>
  );
}

export function Stepper({
  steps,
}: {
  steps: readonly {
    id: string;
    label: string;
    state: "done" | "current" | "waiting";
  }[];
}) {
  return (
    <div className="cd-stepper" aria-label="Steps">
      {steps.map((step, index) => (
        <span key={step.id} className="cd-step" data-state={step.state}>
          {index > 0 ? <span className="cd-step-line" /> : null}
          <span className="cd-step-index">
            {step.state === "done" ? "✓" : index + 1}
          </span>
          <span>{step.label}</span>
        </span>
      ))}
    </div>
  );
}

export function RangeTimeline({
  rows,
}: {
  rows: readonly {
    id: string;
    label: string;
    bars: readonly {
      id: string;
      label: string;
      start: number;
      end: number;
      tone?: "ok" | "info" | "warn" | "bad";
    }[];
  }[];
}) {
  return (
    <div className="cd-timeline">
      {rows.map((row) => (
        <div key={row.id} className="cd-timeline-row">
          <span className="cd-mono">{row.label}</span>
          <div className="cd-timeline-track">
            {row.bars.map((bar) => (
              <span
                key={bar.id}
                className="cd-timeline-bar"
                data-tone={bar.tone ?? "info"}
                title={bar.label}
                style={{
                  left: `${bar.start * 100}%`,
                  width: `${Math.max(2, (bar.end - bar.start) * 100)}%`,
                }}
              />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

export function FacetRail({
  groups,
  selected,
  onToggle,
}: {
  groups: readonly {
    id: string;
    label: string;
    options: readonly { id: string; label: string; count: number }[];
  }[];
  selected: readonly string[];
  onToggle: (id: string) => void;
}) {
  return (
    <div className="cd-facet">
      {groups.map((group) => (
        <div key={group.id}>
          <div className="cd-sec-label">{group.label}</div>
          {group.options.map((option) => (
            <Button
              key={option.id}
              minimal
              small
              active={selected.includes(option.id)}
              alignText="left"
              onClick={() => onToggle(option.id)}
            >
              {option.label} {option.count}
            </Button>
          ))}
        </div>
      ))}
    </div>
  );
}

export function QuickFilterBand({
  options,
  selected,
  onSelect,
}: {
  options: readonly { id: string; label: string }[];
  selected: string;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="cd-chips" role="toolbar" aria-label="Quick filters">
      {options.map((option) => (
        <Tag
          key={option.id}
          interactive
          minimal={option.id !== selected}
          intent={option.id === selected ? "primary" : "none"}
          onClick={() => onSelect(option.id)}
        >
          {option.label}
        </Tag>
      ))}
    </div>
  );
}

export function StatusTabs({
  tabs,
  current,
  onSelect,
}: {
  tabs: readonly {
    id: string;
    label: string;
    count: number;
    intent?: IntentName;
  }[];
  current: string;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="cd-chips" role="tablist">
      {tabs.map((tab) => (
        <Button
          key={tab.id}
          minimal={tab.id !== current}
          intent={tab.id === current ? "primary" : "none"}
          onClick={() => onSelect(tab.id)}
        >
          {tab.label} {tab.count}
        </Button>
      ))}
    </div>
  );
}

export function SegmentedTabs({
  tabs,
  current,
  onSelect,
}: {
  tabs: readonly { id: string; label: string }[];
  current: string;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="cd-segment">
      {tabs.map((tab) => (
        <Button
          key={tab.id}
          small
          outlined={tab.id !== current}
          intent={tab.id === current ? "primary" : "none"}
          onClick={() => onSelect(tab.id)}
        >
          {tab.label}
        </Button>
      ))}
    </div>
  );
}

export function KeyValue({
  items,
}: {
  items: readonly { label: string; value: string }[];
}) {
  return (
    <dl className="cd-kv">
      {items.map((item) => (
        <div key={item.label}>
          <dt>{item.label}</dt>
          <dd className="cd-mono">{item.value || "—"}</dd>
        </div>
      ))}
    </dl>
  );
}

export function StatusChip({
  label,
  intent = "none",
}: {
  label: string;
  intent?: IntentName;
}) {
  return (
    <Tag minimal round intent={intent === "none" ? "none" : intent}>
      {label}
    </Tag>
  );
}

const PRIORITY_INTENT = {
  low: "none",
  medium: "primary",
  high: "warning",
  critical: "danger",
} as const;

export function PriorityTag({
  priority,
}: {
  priority: keyof typeof PRIORITY_INTENT;
}) {
  return (
    <Tag
      minimal
      intent={
        PRIORITY_INTENT[priority] === "none"
          ? "none"
          : PRIORITY_INTENT[priority]
      }
    >
      {priority}
    </Tag>
  );
}

export function GraphView({
  nodes,
  edges,
  selected,
  onSelect,
}: {
  nodes: readonly { id: string; label: string }[];
  edges: readonly { from: string; to: string }[];
  selected?: string;
  onSelect?: (id: string) => void;
}) {
  return (
    <div className="cd-graph" aria-label="Graph">
      {nodes.map((node, index) => (
        <span key={node.id} className="cd-step">
          {index > 0 && edges.some((edge) => edge.to === node.id) ? (
            <span className="cd-step-line" />
          ) : null}
          <Button
            small
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

export function TargetBoard({
  columns,
}: {
  columns: readonly {
    id: string;
    title: string;
    cards: readonly { id: string; title: string; detail: string }[];
  }[];
}) {
  return (
    <div className="cd-board">
      {columns.map((column) => (
        <Card key={column.id} compact>
          <EntityTitle
            title={column.title}
            subtitle={`${column.cards.length}`}
          />
          {column.cards.map((card) => (
            <div key={card.id} className="cd-subpanel">
              <strong>{card.title}</strong>
              <div className="conduit-muted">{card.detail}</div>
            </div>
          ))}
        </Card>
      ))}
    </div>
  );
}

export function ToolApprovalCard({
  title,
  detail,
}: {
  title: string;
  detail: string;
}) {
  return (
    <Card compact>
      <EntityTitle
        title={title}
        subtitle="Example · the thread still owns the live permission"
      />
      <p className="conduit-muted">{detail}</p>
      <Tooltip content="Example card. A live permission is answered on the thread.">
        <Button disabled>Allow</Button>
      </Tooltip>
    </Card>
  );
}

export function SubAgentCard({
  name,
  status,
}: {
  name: string;
  status: string;
}) {
  return (
    <Card compact>
      <EntityTitle title={name} subtitle="Example worker" />
      <StatusChip
        label={status}
        intent={status === "idle" ? "none" : "primary"}
      />
    </Card>
  );
}

export function ClassificationBanner({
  text,
  edge = "inline",
  onDismiss,
}: {
  text: string;
  edge?: "top" | "bottom" | "inline";
  onDismiss?: () => void;
}) {
  return (
    <div className="cd-banner" data-edge={edge} role="note">
      <span>{text}</span>
      {onDismiss ? (
        <Button
          minimal
          small
          icon="cross"
          aria-label="Dismiss banner"
          onClick={onDismiss}
        />
      ) : null}
    </div>
  );
}
