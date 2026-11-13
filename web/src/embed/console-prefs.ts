import type { SearchHit } from "@/components/assistant-ui/elements/conversation-search";
import type { SavedPrompt } from "@/components/assistant-ui/elements/prompt-library";
import type { SearchableThread } from "@/components/assistant-ui/elements/thread-search";

export {
  EMPTY_SETTINGS,
  SETTINGS_KEY,
  loadSettings,
  saveSettings,
} from "../api/console-settings.js";
export type {
  ConsoleSettings,
  KeyValueStore,
} from "../api/console-settings.js";

export const THEME_KEY = "conduit-console-theme";
export const ONBOARDED_KEY = "conduit-console-onboarded";

export const BRIDGE_ENDPOINT = "127.0.0.1";
export const BRIDGE_PATH = "/api/assistant";

export const SHORTCUTS = [
  { keys: "Enter", action: "Send the composer" },
  { keys: "Esc", action: "Stop the running turn" },
  { keys: "Ctrl+Shift+O", action: "New chat" },
  { keys: "Ctrl+K", action: "Command palette" },
  { keys: "Ctrl+Shift+F", action: "Search threads" },
] as const;

export const PACKAGING_PROMPTS: readonly SavedPrompt[] = [
  {
    id: "blockers",
    name: "Packaging blockers",
    body: "Assess this workspace for packaging blockers. List what you can verify and what is unknown. Do not deploy anything.",
    variables: [],
  },
  {
    id: "diffs",
    name: "Review proposed diffs",
    body: "Review the files in this workspace and summarize proposed changes. Do not write outside the workspace and do not deploy.",
    variables: [],
  },
  {
    id: "release",
    name: "Release coordinate",
    body: "Describe the Product and Release coordinate for {{product}}. Keep the coordinate immutable. Do not deploy it.",
    variables: ["product"],
  },
  {
    id: "spoke",
    name: "Spoke boundary",
    body: "Check that execution stays outbound-only for the Spoke in {{environment}}. Do not copy credentials or tenant payloads into the report.",
    variables: ["environment"],
  },
];

export const ONBOARDING_STEPS = [
  {
    title: "This chat stays on this machine",
    body: "The thread talks to the Codex bridge on 127.0.0.1. Nothing is sent until that bridge is up.",
    example: "Assess this workspace for packaging blockers.",
  },
  {
    title: "Each tool use asks first",
    body: "Allow once or reject it. Esc stops the turn. A tool result is not approval to deploy.",
    example: "Review the files in this workspace. Do not deploy.",
  },
  {
    title: "Settings, search, and commands",
    body: "The header opens settings and search. Ctrl+K runs a command, including a new chat or another thread.",
    example: "Ctrl+K",
  },
] as const;

type ThreadRow = {
  id: string;
  title?: string | undefined;
  lastMessageAt?: Date | undefined;
};

export function threadRows(
  threads: readonly ThreadRow[],
  now: Date,
): SearchableThread[] {
  const start = new Date(now);
  start.setHours(0, 0, 0, 0);
  return threads.map((thread) => {
    const title = thread.title?.trim() || "New chat";
    const when = thread.lastMessageAt;
    return {
      id: thread.id,
      title,
      group: !when ? "Conversations" : when >= start ? "Today" : "Earlier",
      preview: when ? `Updated ${when.toLocaleString()}` : "On this bridge",
    };
  });
}

export function conversationHits(
  texts: readonly { id: string; text: string }[],
  query: string,
): SearchHit[] {
  const needle = query.trim().toLowerCase();
  if (needle.length === 0) return [];
  const hits: SearchHit[] = [];
  for (const item of texts) {
    const lower = item.text.toLowerCase();
    let from = 0;
    while (from < item.text.length) {
      const at = lower.indexOf(needle, from);
      if (at === -1) break;
      hits.push({
        id: `${item.id}:${at}`,
        before: item.text.slice(Math.max(0, at - 24), at),
        match: item.text.slice(at, at + needle.length),
        after: item.text.slice(at + needle.length, at + needle.length + 24),
        position: at,
      });
      from = at + needle.length;
    }
  }
  return hits;
}

export function conversationMarkdown(
  title: string,
  turns: readonly { role: string; text: string }[],
): string {
  const body = turns
    .map((turn) => `**${turn.role}**\n\n${turn.text}`)
    .join("\n\n");
  return `# ${title}\n\n${body}\n`;
}
