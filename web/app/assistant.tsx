"use client";

import { useState } from "react";
import { AssistantRuntimeProvider, AuiConfig } from "@assistant-ui/react";
import { McpManagerResource } from "@assistant-ui/react-mcp";
import { Thread } from "@/components/assistant-ui/elements/thread.aui";
import { ConversationMapAui } from "@/components/assistant-ui/elements/conversation-map.aui";
import { useCodexRuntime } from "@/src/api/runtime";
import { AcpPanels } from "@/src/embed/acp-panels";
import { ComposerCommands } from "@/src/embed/composer-commands";
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

export const Assistant = () => {
  const runtime = useCodexRuntime();
  const [view, setView] = useState<"thread" | "elements">("thread");

  return (
    <AssistantRuntimeProvider runtime={runtime} config={ASSISTANT_CONFIG}>
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
              <div className="ml-auto flex items-center gap-1">
                <button
                  type="button"
                  aria-pressed={view === "thread"}
                  className="text-muted-foreground data-[pressed=true]:text-foreground rounded-md px-2 py-1 text-sm"
                  data-pressed={view === "thread"}
                  onClick={() => setView("thread")}
                >
                  Thread
                </button>
                <button
                  type="button"
                  aria-pressed={view === "elements"}
                  className="text-muted-foreground data-[pressed=true]:text-foreground rounded-md px-2 py-1 text-sm"
                  data-pressed={view === "elements"}
                  onClick={() => setView("elements")}
                >
                  Elements
                </button>
              </div>
            </header>
            <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
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
