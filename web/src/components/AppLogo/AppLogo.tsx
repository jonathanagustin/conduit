import { Icon, type IconName } from "@blueprintjs/core";
import type { CSSProperties } from "react";
import "./app-logo.css";

/* AppLogo — the app-identity mark for a surface header: a square tile filled
   with the accent colour at low opacity, holding the icon glyph in the full
   accent colour. Accent is any CSS colour, so each surface carries its own
   identity while sharing one component.

   @provenance ported from packages/foundry/packages/foundry-ui, which
     reconstructs Foundry's `ws-app-header-app-icon` tenant-neutrally. Ported
     from Blueprint 5.19.1 to 6.18.0; the only change is the icon class prefix.
     No private authored-source claim. */
export interface AppLogoProps {
  icon: IconName;
  /** Any CSS colour — the surface's accent. Tile = 15% of it; glyph = the full colour. */
  accent?: string;
  /** Tile edge in px (square). Default 50, matching the header height. */
  size?: number;
  testId?: string;
}

export function AppLogo({
  icon,
  accent = "var(--brand)",
  size = 50,
  testId = "app-logo",
}: AppLogoProps) {
  return (
    <span
      className="console-app-logo"
      style={
        {
          "--console-app-logo-accent": accent,
          "--console-app-logo-size": `${size}px`,
        } as CSSProperties
      }
      data-testid={testId}
    >
      <Icon icon={icon} size={Math.round(size * 0.5)} />
    </span>
  );
}
