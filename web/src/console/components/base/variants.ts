import type { IconName } from "@blueprintjs/core";

/** Axes the base layer accepts. Blueprint receives the mapped props. */
export type IntentName = "none" | "primary" | "success" | "warning" | "danger";
export type SizeName = "sm" | "md" | "lg";
export type DensityName = "compact" | "comfortable";
export type EmphasisName = "minimal" | "outlined" | "filled";
export type SurfaceState = "ready" | "loading" | "empty" | "error" | "disabled-with-reason";
export type TrendName = "up" | "down" | "flat";

export type VariantProps = {
  intent?: IntentName;
  size?: SizeName;
  density?: DensityName;
  emphasis?: EmphasisName;
};

export type BlueprintVariant = {
  intent?: Exclude<IntentName, "none">;
  size: "small" | "medium" | "large";
  compact: boolean;
  minimal: boolean;
  outlined: boolean;
};

const SIZE: Record<SizeName, BlueprintVariant["size"]> = {
  sm: "small",
  md: "medium",
  lg: "large",
};

/** Tag has no small size and no outlined prop. sm stays medium; outlined uses minimal. */
export function blueprintVariant(variant: VariantProps = {}): BlueprintVariant {
  const emphasis = variant.emphasis ?? "filled";
  const intent = variant.intent && variant.intent !== "none" ? variant.intent : undefined;
  return {
    ...(intent ? { intent } : {}),
    size: SIZE[variant.size ?? "md"],
    compact: variant.density === "compact",
    minimal: emphasis === "minimal" || emphasis === "outlined",
    outlined: emphasis === "outlined",
  };
}

export const GATE_PRESET = {
  GO: { intent: "success", emphasis: "filled" },
  CAUTION: { intent: "warning", emphasis: "outlined" },
  "NO-GO": { intent: "danger", emphasis: "filled" },
} as const satisfies Record<string, VariantProps>;

export type GateName = keyof typeof GATE_PRESET;

export const TREND_ICON: Record<TrendName, IconName> = {
  up: "arrow-up",
  down: "arrow-down",
  flat: "minus",
};

export const ELEVATION: Record<EmphasisName, 0 | 1> = {
  minimal: 0,
  outlined: 0,
  filled: 1,
};
