"use client";

import { AssistantRuntimeProvider } from "@assistant-ui/react";
import { useWorkbenchRuntime } from "@/src/console/assistant-runtime";
import { ContextToolUIs } from "@/src/console/context-tools";
import { Workbench } from "@/src/console/Workbench";

/**
 * The console: the operator workbench, with the assistant in its rail. The
 * assistant talks to the Hub (`/v1/assistant/turns`), never a local bridge.
 */
export const Assistant = () => {
  const runtime = useWorkbenchRuntime();
  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ContextToolUIs />
      <div className="dark bp6-dark h-dvh w-full">
        <Workbench dark />
      </div>
    </AssistantRuntimeProvider>
  );
};
