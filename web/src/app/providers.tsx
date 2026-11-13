import { AssistantRuntimeProvider } from "@assistant-ui/react";
import type { ReactNode } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";

import { useCodexRuntime } from "../api/runtime.js";
import { TooltipProvider } from "../components/ui/tooltip.js";

function Runtime({ children }: { children: ReactNode }) {
  const runtime = useCodexRuntime();
  return <AssistantRuntimeProvider runtime={runtime}>{children}</AssistantRuntimeProvider>;
}

export function Providers({ children }: { children: ReactNode }) {
  return (
    <BrowserRouter basename="/console/">
      <TooltipProvider>
        <Routes>
          <Route
            path="*"
            element={<Runtime>{children}</Runtime>}
          />
        </Routes>
      </TooltipProvider>
    </BrowserRouter>
  );
}
