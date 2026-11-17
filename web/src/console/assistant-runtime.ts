"use client";

import {
  createContext,
  useContext,
  useEffect,
  useState,
  useSyncExternalStore,
} from "react";
import {
  useExternalStoreRuntime,
  type AppendMessage,
  type ThreadMessageLike,
} from "@assistant-ui/react";

import { appendAssistantDraft } from "./assistant-draft";
import {
  AssistantStore,
  type AssistantContext,
  type AssistantState,
} from "./assistant-store";
import { toThreadMessage, type ChatMessage } from "./assistant-turn";
import * as assistantApi from "./operator/assistant-api.js";

function assistantContext(): AssistantContext {
  const value = (
    window as Window & { __conduitAssistantContext?: Partial<AssistantContext> }
  ).__conduitAssistantContext;
  return { mode: value?.mode || "explore", chips: value?.chips ?? [] };
}

function browserStorage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

/** The text of a sent message, with a quoted selection as a Markdown quote. */
export function appendedText(message: AppendMessage): string {
  const typed = message.content
    .map((part) => (part.type === "text" ? part.text : ""))
    .join("")
    .trim();
  const quote = (
    message.metadata?.custom as { quote?: { text?: unknown } } | undefined
  )?.quote?.text;
  if (typeof quote !== "string" || !quote.trim()) return typed;
  const quoted = quote
    .trim()
    .split("\n")
    .map((line) => `> ${line}`)
    .join("\n");
  return `${quoted}\n\n${typed}`.trim();
}

const convertMessage = (message: ChatMessage): ThreadMessageLike =>
  toThreadMessage(message) as ThreadMessageLike;

const StoreContext = createContext<AssistantStore | null>(null);

/** Gives the thread list and the tool cards the conversation store. */
export const AssistantStoreProvider = StoreContext.Provider;

export function useAssistantStore(): AssistantStore {
  const store = useContext(StoreContext);
  if (!store) throw new Error("the assistant store is not mounted");
  return store;
}

export function useAssistantState<T>(select: (state: AssistantState) => T): T {
  const store = useAssistantStore();
  return select(
    useSyncExternalStore(store.subscribe, store.getState, store.getState),
  );
}

/**
 * The workbench assistant's runtime.
 *
 * It is assistant-ui's external-store runtime over AssistantStore rather than
 * useLocalRuntime or the remote thread-list runtime: the Hub owns the
 * conversation (threads, stored tool calls, error codes), the turn arrives as
 * AG-UI events this console already parses, and the local runtime would keep
 * its own copy of the messages that a reload loses. The external store lets
 * the store hand over exactly what the Hub holds, including a failed turn as
 * an incomplete message, and gates edit, branch, attachment and speech
 * controls off because no handler for them is given.
 */
export function useWorkbenchRuntime() {
  const [store] = useState(
    () =>
      new AssistantStore({
        api: assistantApi,
        context: assistantContext,
        storage: browserStorage(),
      }),
  );
  const state = useSyncExternalStore(
    store.subscribe,
    store.getState,
    store.getState,
  );
  useEffect(() => {
    void store.init();
  }, [store]);

  const runtime = useExternalStoreRuntime<ChatMessage>({
    messages: state.messages,
    isRunning: state.running,
    isLoading: state.loading,
    isDisabled: state.loading,
    convertMessage,
    onNew: (message) => store.send(appendedText(message)),
    onCancel: async () => store.cancel(),
    onReload: () => store.retry(),
  });

  useEffect(() => {
    const addDraft = (event: Event) => {
      const detail = (
        event as CustomEvent<{ text?: unknown; accepted?: boolean }>
      ).detail;
      if (!detail || typeof detail !== "object") return;
      const next = appendAssistantDraft(
        runtime.thread.composer.getState().text,
        detail.text,
      );
      if (next === null) return;
      runtime.thread.composer.setText(next);
      detail.accepted = true;
    };
    window.addEventListener("conduit-assistant-draft", addDraft);
    return () =>
      window.removeEventListener("conduit-assistant-draft", addDraft);
  }, [runtime]);
  return { runtime, store };
}
