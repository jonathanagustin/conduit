"use client";

import { AssistantRuntimeProvider } from "@assistant-ui/react";
import { useAcpRuntime } from "@/lib/acp";
import { useState } from "react";

import {
  AgentStatus,
  TaskTray,
} from "@/components/assistant-ui/elements/agent-status.aui";
import { Thread } from "@/components/assistant-ui/elements/thread.aui";
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

export const Assistant = ({ demo = false }: { demo?: boolean }) => {
  const [error, setError] = useState<string | null>(null);
  const runtime = useAcpRuntime({
    url:
      typeof window === "undefined"
        ? "ws://localhost:3000/console/acp"
        : `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}/console/acp`,
    cwd: "/",
    permissions: "ask",
    onError: (failure) =>
      setError(failure instanceof Error ? failure.message : String(failure)),
  });

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <SidebarProvider>
        {demo && (
          <div
            role="status"
            className="fixed inset-x-0 top-0 z-50 bg-amber-100 px-4 py-2 text-center text-sm text-amber-950"
          >
            Simulation: fixture data only. No model calls, commands, or
            deployments.
          </div>
        )}
        <div className={`flex h-dvh w-full pr-0.5 ${demo ? "pt-10" : ""}`}>
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
                    <BreadcrumbPage>Conduit Assistant</BreadcrumbPage>
                  </BreadcrumbItem>
                </BreadcrumbList>
              </Breadcrumb>
              <div className="ml-auto flex items-center gap-2">
                <AgentStatus />
                <TaskTray />
              </div>
            </header>
            {error && (
              <p
                role="alert"
                className="text-destructive border-b px-4 py-2 text-sm"
              >
                {error}
              </p>
            )}
            <div className="flex-1 overflow-hidden">
              <Thread />
            </div>
          </SidebarInset>
        </div>
      </SidebarProvider>
    </AssistantRuntimeProvider>
  );
};
