export const SETTINGS_KEY = "conduit-console-settings";

export type KeyValueStore = {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
};

export type ConsoleSettings = {
  model: string;
  effort: string;
  systemPrompt: string;
  temperature: number;
};

export const EMPTY_SETTINGS: ConsoleSettings = {
  model: "",
  effort: "",
  systemPrompt: "",
  temperature: 0,
};

export function loadSettings(store: KeyValueStore): ConsoleSettings {
  const raw = store.getItem(SETTINGS_KEY);
  if (!raw) return { ...EMPTY_SETTINGS };
  try {
    const parsed = JSON.parse(raw) as Partial<ConsoleSettings>;
    const temperature =
      typeof parsed.temperature === "number" &&
      Number.isFinite(parsed.temperature)
        ? Math.min(2, Math.max(0, parsed.temperature))
        : 0;
    return {
      model: typeof parsed.model === "string" ? parsed.model : "",
      effort: typeof parsed.effort === "string" ? parsed.effort : "",
      systemPrompt:
        typeof parsed.systemPrompt === "string"
          ? parsed.systemPrompt.slice(0, 2000)
          : "",
      temperature,
    };
  } catch {
    return { ...EMPTY_SETTINGS };
  }
}

export function saveSettings(
  store: KeyValueStore,
  settings: ConsoleSettings,
): void {
  store.setItem(SETTINGS_KEY, JSON.stringify(settings));
}
