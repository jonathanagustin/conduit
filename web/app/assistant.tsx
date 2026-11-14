"use client";

import { useEffect, useState } from "react";
import { AssistantRuntimeProvider, AuiConfig } from "@assistant-ui/react";
import { McpManagerResource } from "@assistant-ui/react-mcp";
import { Thread } from "@/components/assistant-ui/elements/thread.aui";
import { ConversationMapAui } from "@/components/assistant-ui/elements/conversation-map.aui";
import { useCodexRuntime } from "@/src/api/runtime";
import { AcpPanels } from "@/src/embed/acp-panels";
import { ComposerCommands } from "@/src/embed/composer-commands";
import { ThreadWelcome } from "@/src/embed/thread-welcome";
import { BridgeConnection, ConsoleChrome } from "@/src/embed/console-shell";
import { ContextToolUIs } from "@/src/console/context-tools";
import { Workbench } from "@/src/console/Workbench";
import { ElementCatalog } from "@/src/embed/element-catalog";
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import { ThreadListSidebar } from "@/components/assistant-ui/elements/threadlist-sidebar.aui";
import { Separator } from "@/components/ui/separator";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";

const THREAD_COMPONENTS = {
  ViewportHeader: AcpPanels,
  ComposerExtra: ComposerCommands,
  Welcome: ThreadWelcome,
};

const ASSISTANT_CONFIG = AuiConfig({
  mcp: McpManagerResource({
    autoConnect: false,
    connectors: [
      {
        id: "demo",
        name: "Demo connector",
        url: "https://example.com/mcp",
        auth: { type: "none" },
      },
    ],
  }),
});

const THEME_KEY = "conduit-console-theme";

export const Assistant = () => {
  const runtime = useCodexRuntime();
  const [view, setView] = useState<"thread" | "elements" | "workbench">("thread");
  const [dark, setDark] = useState(false);

  useEffect(() => {
    const stored = window.localStorage.getItem(THEME_KEY);
    const next =
      stored === "dark" ||
      (stored !== "light" &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);
    setDark(next);
    document.documentElement.classList.toggle("dark", next);
  }, []);

  const toggleTheme = () => {
    setDark((current) => {
      const next = !current;
      document.documentElement.classList.toggle("dark", next);
      window.localStorage.setItem(THEME_KEY, next ? "dark" : "light");
      return next;
    });
  };

  if (view === "workbench") {
    return (
      <AssistantRuntimeProvider runtime={runtime} config={ASSISTANT_CONFIG}>
        <ContextToolUIs />
        <div className="dark bp6-dark h-dvh w-full">
          <Workbench dark />
        </div>
      </AssistantRuntimeProvider>
    );
  }

  return (
    <AssistantRuntimeProvider runtime={runtime} config={ASSISTANT_CONFIG}>
      <ContextToolUIs />
      <SidebarProvider>
        <div className="flex h-dvh w-full pr-0.5">
          <ThreadListSidebar />
          <SidebarInset>
            <header className="flex h-16 shrink-0 items-center gap-2 border-b px-4">
              <SidebarTrigger />
              <Separator orientation="vertical" className="mr-2 h-4" />
              <Breadcrumb>
                <BreadcrumbList>
                  <BreadcrumbItem className="hidden md:block">
                    <BreadcrumbLink
                      href="https://www.assistant-ui.com/docs/getting-started"
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      Build Your Own ChatGPT UX
                    </BreadcrumbLink>
                  </BreadcrumbItem>
                  <BreadcrumbSeparator className="hidden md:block" />
                  <BreadcrumbItem>
                    <BreadcrumbPage>Starter Template</BreadcrumbPage>
                  </BreadcrumbItem>
                </BreadcrumbList>
              </Breadcrumb>
              <p className="text-muted-foreground hidden text-xs xl:block">
                Enter sends, Esc stops, Ctrl+K commands, Ctrl+Shift+O new chat
              </p>
              <div className="ml-auto flex items-center gap-1">
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground rounded-md px-2 py-1 text-sm"
                  aria-pressed={dark}
                  onClick={toggleTheme}
                >
                  {dark ? "Light" : "Dark"}
                </button>
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground rounded-md px-2 py-1 text-sm"
                  aria-pressed={false}
                  onClick={() => setView("workbench")}
                >
                  Workbench
                </button>
                <ConsoleChrome
                  view={view}
                  onView={setView}
                  dark={dark}
                  onToggleTheme={toggleTheme}
                />
              </div>
            </header>
            <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
              <BridgeConnection />
              {view === "thread" ? (
                <div className="relative min-h-0 flex-1">
                  <ConversationMapAui side="right" />
                  <Thread components={THREAD_COMPONENTS} />
                </div>
              ) : (
                <ElementCatalog />
              )}
            </div>
          </SidebarInset>
        </div>
      </SidebarProvider>
    </AssistantRuntimeProvider>
  );
};
