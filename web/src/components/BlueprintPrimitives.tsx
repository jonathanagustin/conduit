import {
  CardList as BpCardList,
  EntityTitle as BpEntityTitle,
  Switch as BpSwitch,
  Tag as BpTag,
  type Intent,
} from "@blueprintjs/core";
import type { ComponentProps, ReactNode } from "react";

import "./conduit-primitives.css";

export type BlueprintEntityIcon = ComponentProps<typeof BpEntityTitle>["icon"];
export type BlueprintEntityHeading = ComponentProps<
  typeof BpEntityTitle
>["heading"];

export interface ConduitEntityTitleProps {
  readonly title: string;
  readonly subtitle?: string;
  readonly tags?: ReactNode;
  readonly icon?: BlueprintEntityIcon;
  readonly fill?: boolean;
  readonly wrap?: boolean;
  readonly heading?: BlueprintEntityHeading;
  readonly actions?: ReactNode;
  readonly className?: string;
}

export function ConduitEntityTitle({
  title,
  subtitle,
  tags,
  icon,
  fill = false,
  wrap = false,
  heading,
  actions,
  className,
}: ConduitEntityTitleProps) {
  const classes = [
    fill ? "cnd-entity-title-fill" : "",
    wrap ? "cnd-entity-title-wrap" : "",
    className ?? "",
  ]
    .filter(Boolean)
    .join(" ");

  const entity = (
    <BpEntityTitle
      title={title}
      subtitle={subtitle}
      tags={tags}
      icon={icon}
      fill={fill}
      heading={heading}
      className={classes || undefined}
    />
  );

  if (!actions) {
    return entity;
  }

  return (
    <header className="cnd-entity-header">
      {entity}
      <div className="cnd-entity-header-actions">{actions}</div>
    </header>
  );
}

export interface ConduitCardListProps {
  readonly bordered?: boolean;
  readonly compact?: boolean;
  readonly children?: ReactNode;
  readonly ariaLabel?: string;
  readonly className?: string;
}

export function ConduitCardList({
  bordered = true,
  compact = false,
  children,
  ariaLabel,
  className,
}: ConduitCardListProps) {
  return (
    <BpCardList
      bordered={bordered}
      compact={compact}
      aria-label={ariaLabel}
      className={className}
    >
      {children}
    </BpCardList>
  );
}

export interface ConduitSwitchCardProps {
  readonly label: string;
  readonly helperText?: string;
  readonly checked: boolean;
  readonly disabled?: boolean;
  readonly onChange: (checked: boolean) => void;
  readonly className?: string;
}

export function ConduitSwitchCard({
  label,
  helperText,
  checked,
  disabled = false,
  onChange,
  className,
}: ConduitSwitchCardProps) {
  return (
    <div
      className={["cnd-switch-card", className ?? ""].filter(Boolean).join(" ")}
    >
      <div className="cnd-switch-card-info">
        <span className="cnd-switch-card-label">{label}</span>
        {helperText ? (
          <span className="cnd-switch-card-helper">{helperText}</span>
        ) : null}
      </div>
      <BpSwitch
        checked={checked}
        disabled={disabled}
        innerLabelChecked="on"
        innerLabel="off"
        onChange={(event) => onChange(event.currentTarget.checked)}
      />
    </div>
  );
}

export interface KeyComboTagProps {
  readonly combo: string;
  readonly className?: string;
}

export function KeyComboTag({ combo, className }: KeyComboTagProps) {
  return (
    <BpTag minimal className={className}>
      {combo}
    </BpTag>
  );
}

export interface OrbCountBadgeProps {
  readonly count: number;
  readonly intent?: Intent;
  readonly max?: number;
  readonly className?: string;
}

export function OrbCountBadge({
  count,
  intent = "none",
  max = 99,
  className,
}: OrbCountBadgeProps) {
  const displayCount = count > max ? `${max}+` : String(count);
  const classes = ["cnd-orb-badge", className ?? ""].filter(Boolean).join(" ");
  return (
    <BpTag round intent={intent} className={classes}>
      {displayCount}
    </BpTag>
  );
}
