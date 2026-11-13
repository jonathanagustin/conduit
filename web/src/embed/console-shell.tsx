"use client";

import { useAui, useAuiState } from "@assistant-ui/react";
import { useEffect, useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { CommandPalette } from "@/components/assistant-ui/elements/command-palette";
import type { PaletteCommand } from "@/components/assistant-ui/elements/command-palette";
import { ConnectionState } from "@/components/assistant-ui/elements/connection-state";
import { ConversationSearch } from "@/components/assistant-ui/elements/conversation-search";
import { PromptLibrary } from "@/components/assistant-ui/elements/prompt-library";
import {
  SettingsPanel,
  type SettingToggle,
} from "@/components/assistant-ui/elements/settings-panel";
import { SharedConversation } from "@/components/assistant-ui/elements/shared-conversation";
import type { SharedTurn } from "@/components/assistant-ui/elements/shared-conversation";
import { ThreadSearch } from "@/components/assistant-ui/elements/thread-search";
import { field, mono } from "@/components/assistant-ui/elements/surfaces";
import {
  rememberSessionChoice,
  defaultSessionConfig,
  sessionConfigOf,
  setSessionConfig,
} from "@/src/api/client";
import type { SessionConfigOption } from "@/src/api/protocol";
import { useBridgeHealth } from "@/src/embed/bridge-health";
import {
  BRIDGE_ENDPOINT,
  BRIDGE_PATH,
  conversationHits,
  conversationMarkdown,
  loadSettings,
  PACKAGING_PROMPTS,
  saveSettings,
  SHORTCUTS,
  threadRows,
  type ConsoleSettings,
} from "@/src/embed/console-prefs";
import { EFFORT_FALLBACK } from "@/src/embed/session-controls";
import { cn } from "@/lib/utils";

type ConsoleChromeProps = {
  view: "thread" | "elements";
  onView: (view: "thread" | "elements") => void;
  dark: boolean;
  onToggleTheme: () => void;
};

type Panel =
  | "settings"
  | "palette"
  | "threads"
  | "prompts"
  | "find"
  | "share"
  | null;

function selectOption(options: readonly SessionConfigOption[], id: string) {
  return options.find((option) => option.id === id && option.type === "select");
}

function messageText(message: {
  content: readonly { type: string; text?: string }[];
}): string {
  return message.content
    .flatMap((part) =>
      part.type === "text" && typeof part.text === "string" ? [part.text] : [],
    )
    .join("\n");
}

function downloadMarkdown(title: string, markdown: string): void {
  const blob = new Blob([markdown], { type: "text/markdown" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `${title.replace(/[^\w.-]+/g, "-").slice(0, 40) || "conversation"}.md`;
  link.click();
  URL.revokeObjectURL(url);
}

export function BridgeConnection() {
  const health = useBridgeHealth();
  useEffect(() => {
    void health.check();
  }, [health.check]);
  if (health.phase === "online") return null;
  return (
    <div className="flex justify-center px-4 pt-3">
      <ConnectionState
        phase={health.phase}
        attempt={health.attempt}
        onRetry={() => {
          void health.check();
        }}
      />
    </div>
  );
}

export function ConsoleChrome({
  view,
  onView,
  dark,
  onToggleTheme,
}: ConsoleChromeProps) {
  const aui = useAui();
  const health = useBridgeHealth();
  const remoteId = useAuiState((state) => state.threadListItem.remoteId);
  const threadTitle = useAuiState(
    (state) => state.threadListItem.title ?? "New chat",
  );
  const threadItems = useAuiState((state) => state.threads.threadItems);
  const messages = useAuiState((state) => state.thread.messages);
  const [panel, setPanel] = useState<Panel>(null);
  const [settings, setSettings] = useState<ConsoleSettings>(() =>
    loadSettings(window.localStorage),
  );
  const [options, setOptions] = useState<SessionConfigOption[]>([]);
  const [paletteQuery, setPaletteQuery] = useState("");
  const [paletteActive, setPaletteActive] = useState("new");
  const [threadQuery, setThreadQuery] = useState("");
  const [threadActive, setThreadActive] = useState("");
  const [promptQuery, setPromptQuery] = useState("");
  const [promptId, setPromptId] = useState(PACKAGING_PROMPTS[0]?.id ?? "");
  const [findQuery, setFindQuery] = useState("");
  const [findIndex, setFindIndex] = useState(0);

  const threadId =
    remoteId && !remoteId.startsWith("offline-") ? remoteId : undefined;

  useEffect(() => {
    let cancelled = false;
    const load = threadId ? sessionConfigOf(threadId) : defaultSessionConfig();
    void load
      .then((next) => {
        if (!cancelled) setOptions(next);
      })
      .catch(() => {
        if (!cancelled) setOptions([]);
      });
    return () => {
      cancelled = true;
    };
  }, [threadId, panel]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const meta = event.metaKey || event.ctrlKey;
      if (meta && !event.shiftKey && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setPanel((current) => (current === "palette" ? null : "palette"));
        setPaletteQuery("");
        setPaletteActive("new");
        return;
      }
      if (meta && event.shiftKey && event.key.toLowerCase() === "f") {
        event.preventDefault();
        setPanel("threads");
        setThreadQuery("");
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    if (panel !== "palette") return;
    document
      .querySelector<HTMLInputElement>("[data-slot=command-palette] input")
      ?.focus();
  }, [panel]);

  const modelOption = selectOption(options, "model");
  const effortOption = selectOption(options, "reasoning_effort");
  const modelLabels = modelOption?.options?.map((option) => option.name) ?? [
    "Codex",
  ];
  const selectedModel =
    modelOption?.options?.find((option) => option.value === settings.model)
      ?.name ??
    modelOption?.options?.find(
      (option) => option.value === modelOption.currentValue,
    )?.name ??
    modelLabels[0] ??
    "Codex";

  const rows = useMemo(
    () => threadRows(threadItems, new Date()),
    [threadItems],
  );
  const turns = useMemo(() => {
    return messages.flatMap((message) => {
      const text = messageText(message);
      if (text.length === 0) return [];
      return [{ id: message.id, role: message.role, text }];
    });
  }, [messages]);
  const hits = useMemo(
    () => conversationHits(turns, findQuery),
    [turns, findQuery],
  );
  const sharedTurns: SharedTurn[] = turns
    .filter((turn) => turn.role === "user" || turn.role === "assistant")
    .map((turn) => ({
      id: turn.id,
      role: turn.role === "user" ? "user" : "assistant",
      text: turn.text,
    }));

  const persist = (next: ConsoleSettings) => {
    setSettings(next);
    saveSettings(window.localStorage, next);
  };

  const applyModel = (name: string) => {
    const match = modelOption?.options?.find((option) => option.name === name);
    if (!match) return;
    persist({ ...settings, model: match.value });
    rememberSessionChoice({ model: match.value });
    if (!threadId) return;
    void setSessionConfig(threadId, "model", match.value)
      .then(setOptions)
      .catch(() => undefined);
  };

  const applyEffort = (value: string) => {
    persist({ ...settings, effort: value });
    rememberSessionChoice({ effort: value });
    if (!threadId) return;
    void setSessionConfig(threadId, "reasoning_effort", value)
      .then(setOptions)
      .catch(() => undefined);
  };

  const toggles: SettingToggle[] = [
    {
      key: "theme",
      label: "Dark theme",
      detail: "Use the dark palette on this browser",
      on: dark,
    },
  ];

  const commands: PaletteCommand[] = [
    {
      id: "new",
      label: "New chat",
      group: "Thread",
      keys: ["Ctrl", "Shift", "O"],
    },
    {
      id: "search",
      label: "Search threads",
      group: "Thread",
      keys: ["Ctrl", "Shift", "F"],
    },
    { id: "find", label: "Find in conversation", group: "Thread", keys: [] },
    { id: "prompts", label: "Prompt library", group: "Thread", keys: [] },
    { id: "export", label: "Export conversation", group: "Thread", keys: [] },
    ...threadItems.map((item) => ({
      id: `thread:${item.id}`,
      label: item.title?.trim() || "New chat",
      group: "Switch thread",
      keys: [],
    })),
    {
      id: "theme",
      label: dark ? "Use light theme" : "Use dark theme",
      group: "App",
      keys: [],
    },
    { id: "settings", label: "Open settings", group: "App", keys: [] },
    { id: "elements", label: "Open Elements", group: "App", keys: [] },
    { id: "thread", label: "Open thread", group: "App", keys: [] },
  ];

  const runCommand = (id: string) => {
    if (id === "new") {
      aui.threads.switchToNewThread();
      onView("thread");
      setPanel(null);
      return;
    }
    if (id === "search") {
      setPanel("threads");
      return;
    }
    if (id === "find") {
      setPanel("find");
      setFindQuery("");
      setFindIndex(0);
      return;
    }
    if (id === "prompts") {
      setPanel("prompts");
      return;
    }
    if (id === "export") {
      const markdown = conversationMarkdown(threadTitle, sharedTurns);
      downloadMarkdown(threadTitle, markdown);
      if (sharedTurns.length > 0) setPanel("share");
      else setPanel(null);
      return;
    }
    if (id === "theme") {
      onToggleTheme();
      setPanel(null);
      return;
    }
    if (id === "settings") {
      setPanel("settings");
      return;
    }
    if (id === "elements") {
      onView("elements");
      setPanel(null);
      return;
    }
    if (id === "thread") {
      onView("thread");
      setPanel(null);
      return;
    }
    if (id.startsWith("thread:")) {
      aui.threads.switchToThread(id.slice("thread:".length));
      onView("thread");
      setPanel(null);
    }
  };

  const clearThreads = () => {
    const ids = aui.threads.getState().threadItems.map((item) => item.id);
    for (const id of ids) aui.threads.item({ id }).delete();
    aui.threads.switchToNewThread();
    onView("thread");
    setPanel(null);
  };

  const headerButton =
    "text-muted-foreground hover:text-foreground rounded-md px-2 py-1 text-sm";

  return (
    <>
      <button
        type="button"
        className={headerButton}
        onClick={() => {
          setThreadQuery("");
          setThreadActive(rows[0]?.id ?? "");
          setPanel("threads");
        }}
      >
        Search
      </button>
      <button
        type="button"
        className={headerButton}
        onClick={() => setPanel("prompts")}
      >
        Prompts
      </button>
      <button
        type="button"
        className={headerButton}
        aria-pressed={view === "thread"}
        data-pressed={view === "thread"}
        onClick={() => onView("thread")}
      >
        Thread
      </button>
      <button
        type="button"
        className={headerButton}
        aria-pressed={view === "elements"}
        data-pressed={view === "elements"}
        onClick={() => onView("elements")}
      >
        Elements
      </button>
      <button
        type="button"
        className={headerButton}
        onClick={() => setPanel("settings")}
      >
        Settings
      </button>

      <Dialog
        open={panel === "settings"}
        onOpenChange={(open) => {
          if (!open)
            setPanel((current) => (current === "settings" ? null : current));
        }}
      >
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Settings</DialogTitle>
            <DialogDescription>
              Defaults for this browser. The model and reasoning effort are sent
              with the next ACP session.
            </DialogDescription>
          </DialogHeader>
          <SettingsPanel
            model={selectedModel}
            models={modelLabels}
            systemPrompt={settings.systemPrompt}
            temperature={settings.temperature}
            toggles={toggles}
            onModelChange={applyModel}
            onSystemPromptChange={(systemPrompt) =>
              persist({ ...settings, systemPrompt })
            }
            onTemperatureChange={(temperature) =>
              persist({ ...settings, temperature })
            }
            onToggle={(key) => {
              if (key === "theme") onToggleTheme();
            }}
          />
          <p className="text-muted-foreground text-xs leading-relaxed">
            The system prompt and temperature stay in this browser. Codex ACP
            does not expose either as a session config option, so they are not
            sent.
          </p>
          <div className="flex flex-col gap-1.5">
            <span className={cn(mono, "text-muted-foreground")}>
              reasoning effort
            </span>
            {effortOption?.options && effortOption.options.length > 0 ? (
              <div className={cn(field, "flex gap-0.5 rounded-full p-0.5")}>
                {effortOption.options.map((option) => {
                  const on =
                    (settings.effort || effortOption.currentValue) ===
                    option.value;
                  return (
                    <button
                      key={option.value}
                      type="button"
                      aria-pressed={on}
                      onClick={() => applyEffort(option.value)}
                      className={cn(
                        "flex-1 rounded-full py-1 text-xs font-medium",
                        on
                          ? "bg-background text-foreground/90"
                          : "text-muted-foreground hover:text-foreground/70",
                      )}
                    >
                      {option.name}
                    </button>
                  );
                })}
              </div>
            ) : (
              <p className="text-muted-foreground text-xs">{EFFORT_FALLBACK}</p>
            )}
          </div>
          <div className="flex flex-col gap-1">
            <span className={cn(mono, "text-muted-foreground")}>shortcuts</span>
            <ul className="flex flex-col gap-1">
              {SHORTCUTS.map((shortcut) => (
                <li
                  key={shortcut.keys}
                  className="flex items-baseline justify-between gap-3 text-xs"
                >
                  <span>{shortcut.action}</span>
                  <span className={cn(mono, "text-muted-foreground")}>
                    {shortcut.keys}
                  </span>
                </li>
              ))}
            </ul>
          </div>
          <div className="flex flex-col gap-1">
            <span className={cn(mono, "text-muted-foreground")}>bridge</span>
            <p className="text-xs">
              {BRIDGE_ENDPOINT} · {BRIDGE_PATH}
            </p>
            <p className="text-muted-foreground text-xs">
              {health.info
                ? `${health.info.runtime} · ${health.info.mode}${health.info.fixture ? " · fixture" : ""}`
                : health.phase === "dropped"
                  ? "Unreachable"
                  : "Checking"}
            </p>
          </div>
          <Button type="button" variant="outline" onClick={clearThreads}>
            Clear conversations
          </Button>
        </DialogContent>
      </Dialog>

      <Dialog
        open={panel === "palette"}
        onOpenChange={(open) => {
          if (!open)
            setPanel((current) => (current === "palette" ? null : current));
        }}
      >
        <DialogContent className="sm:max-w-md" showCloseButton={false}>
          <DialogHeader className="sr-only">
            <DialogTitle>Commands</DialogTitle>
            <DialogDescription>Run a console command.</DialogDescription>
          </DialogHeader>
          <CommandPalette
            commands={commands}
            query={paletteQuery}
            activeId={paletteActive}
            onQueryChange={setPaletteQuery}
            onActiveChange={setPaletteActive}
            onRun={runCommand}
          />
        </DialogContent>
      </Dialog>

      <Dialog
        open={panel === "threads"}
        onOpenChange={(open) => {
          if (!open)
            setPanel((current) => (current === "threads" ? null : current));
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Search threads</DialogTitle>
            <DialogDescription>
              Open a conversation from this bridge.
            </DialogDescription>
          </DialogHeader>
          <ThreadSearch
            threads={rows}
            query={threadQuery}
            activeId={threadActive || rows[0]?.id || ""}
            onQueryChange={setThreadQuery}
            onActiveChange={setThreadActive}
            onSelect={(id) => {
              aui.threads.switchToThread(id);
              onView("thread");
              setPanel(null);
            }}
          />
        </DialogContent>
      </Dialog>

      <Dialog
        open={panel === "prompts"}
        onOpenChange={(open) => {
          if (!open)
            setPanel((current) => (current === "prompts" ? null : current));
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Prompt library</DialogTitle>
            <DialogDescription>
              Insert a packaging prompt into the composer.
            </DialogDescription>
          </DialogHeader>
          <PromptLibrary
            prompts={PACKAGING_PROMPTS}
            query={promptQuery}
            selectedId={promptId}
            onQueryChange={setPromptQuery}
            onSelect={setPromptId}
            onInsert={(id) => {
              const prompt = PACKAGING_PROMPTS.find((item) => item.id === id);
              if (!prompt) return;
              onView("thread");
              aui.composer.setText(prompt.body);
              setPanel(null);
            }}
          />
        </DialogContent>
      </Dialog>

      <Dialog
        open={panel === "find"}
        onOpenChange={(open) => {
          if (!open)
            setPanel((current) => (current === "find" ? null : current));
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Find in conversation</DialogTitle>
            <DialogDescription>
              Search the messages in the open thread.
            </DialogDescription>
          </DialogHeader>
          <ConversationSearch
            query={findQuery}
            hits={hits}
            activeIndex={findIndex}
            onQueryChange={(query) => {
              setFindQuery(query);
              setFindIndex(0);
            }}
            onStep={(delta) => {
              if (hits.length === 0) return;
              const next = (findIndex + delta + hits.length) % hits.length;
              setFindIndex(next);
              const hit = hits[next];
              if (!hit) return;
              const root = document.querySelector(
                "[data-slot=aui_thread-viewport]",
              );
              const nodes = root?.querySelectorAll(
                "[data-slot=aui_assistant-message-root], [data-slot=aui_user-message-root]",
              );
              for (const node of nodes ?? []) {
                if (
                  node.textContent
                    ?.toLowerCase()
                    .includes(hit.match.toLowerCase())
                ) {
                  node.scrollIntoView({ block: "center" });
                  break;
                }
              }
            }}
          />
        </DialogContent>
      </Dialog>

      <Dialog
        open={panel === "share"}
        onOpenChange={(open) => {
          if (!open)
            setPanel((current) => (current === "share" ? null : current));
        }}
      >
        <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Export</DialogTitle>
            <DialogDescription>
              A markdown copy was downloaded.
            </DialogDescription>
          </DialogHeader>
          <SharedConversation
            title={threadTitle}
            sharedBy="this browser"
            sharedAt={new Date().toLocaleDateString()}
            turns={sharedTurns}
            onContinue={() => {
              onView("thread");
              setPanel(null);
            }}
          />
        </DialogContent>
      </Dialog>
    </>
  );
}
