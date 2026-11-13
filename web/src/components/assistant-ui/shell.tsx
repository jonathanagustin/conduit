import type { ConsoleBootstrap } from "../../runtime/bootstrap.js";
import type { ReactNode } from "react";

export function AssistantShell({
  bootstrap,
  bridge,
  children,
}: {
  bootstrap: ConsoleBootstrap;
  bridge: string;
  children: ReactNode;
}) {
  return (
    <div className="flex h-full min-h-0 flex-col" data-testid="assistant-shell">
      <header className="flex items-center justify-between border-b border-border px-4 py-3">
        <div>
          <p className="text-sm font-semibold" data-testid="tenant-title">
            {bootstrap.brand.title}
          </p>
          <p className="text-xs text-muted-foreground">{bootstrap.brand.subtitle}</p>
        </div>
        <p className="text-xs text-muted-foreground" data-testid="bridge-status">
          {bridge}
        </p>
      </header>
      <div className="flex min-h-0 flex-1">{children}</div>
    </div>
  );
}
