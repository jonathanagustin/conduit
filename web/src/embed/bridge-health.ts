"use client";

import { useEffect, useState } from "react";

import type { ConnectionPhase } from "@/components/assistant-ui/elements/connection-state";
import { bridgeInfo, type BridgeInfo } from "@/src/api/client";

type Health = {
  phase: ConnectionPhase;
  info: BridgeInfo | null;
  attempt: number;
};

let health: Health = { phase: "online", info: null, attempt: 0 };
const listeners = new Set<() => void>();
let inflight: Promise<void> | undefined;

function publish(next: Health): void {
  health = next;
  for (const listener of listeners) listener();
}

export function refreshBridgeHealth(): Promise<void> {
  if (inflight) return inflight;
  const previous = health.phase;
  if (previous === "dropped") {
    publish({ ...health, phase: "reconnecting", attempt: health.attempt + 1 });
  }
  inflight = bridgeInfo()
    .then((info) => {
      const recovered = previous === "dropped" || previous === "reconnecting";
      publish({
        phase: recovered ? "resumed" : "online",
        info,
        attempt: health.attempt,
      });
    })
    .catch(() => {
      publish({
        phase: "dropped",
        info: null,
        attempt: health.attempt + 1,
      });
    })
    .finally(() => {
      inflight = undefined;
    });
  return inflight;
}

export function useBridgeHealth(): Health & { check: () => Promise<void> } {
  const [state, setState] = useState(health);
  useEffect(() => {
    const listener = () => setState(health);
    listeners.add(listener);
    setState(health);
    return () => {
      listeners.delete(listener);
    };
  }, []);
  useEffect(() => {
    if (state.phase !== "resumed") return;
    const timer = window.setTimeout(() => {
      if (health.phase === "resumed") publish({ ...health, phase: "online" });
    }, 2000);
    return () => window.clearTimeout(timer);
  }, [state.phase]);
  return { ...state, check: refreshBridgeHealth };
}
