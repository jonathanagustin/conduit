"use client";

import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import "@fontsource/inter/400.css";
import "@fontsource/inter/500.css";
import "@fontsource/inter/600.css";
import "@fontsource/inter/700.css";
import "@fontsource/jetbrains-mono/400.css";
import "@fontsource/jetbrains-mono/500.css";
import "@fontsource/jetbrains-mono/600.css";

import { Thread } from "@/components/assistant-ui/elements/thread.aui";
import { AcpPanels } from "@/src/embed/acp-panels";

import { mountOperator } from "./operator/engine.js";
import "./operator/console.css";

const RAIL_THREAD = {
  ViewportHeader: AcpPanels,
  Welcome: function RailWelcome() {
    return null;
  },
};

export function Workbench({ dark }: { dark: boolean }) {
  void dark;
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  const onAssistantMount = useCallback((element: HTMLElement | null) => {
    setSlot((current) => (current === element ? current : element));
  }, []);

  useEffect(() => {
    const host = document.getElementById("operator-root");
    if (!host) return;
    return mountOperator(host, { onAssistantMount });
  }, [onAssistantMount]);

  return (
    <div id="operator-root" className="operator-root bp6-dark flex h-full w-full flex-col overflow-hidden">
      {slot
        ? createPortal(
            <div className="flex h-full min-h-0 flex-col">
              <Thread components={RAIL_THREAD} />
            </div>,
            slot,
          )
        : null}
    </div>
  );
}
