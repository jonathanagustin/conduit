export type ServedModel = {
  kind?: string;
  model?: string;
  configuredModel?: string;
  providerModel?: string;
};

/** Display configuration and response metadata without inferring an identity. */
export function servedModelLabel(served?: ServedModel): string {
  const configured = served?.configuredModel?.trim();
  const provider = served?.providerModel?.trim();
  if (provider) {
    return configured && configured !== provider
      ? `Model: ${provider} (configured: ${configured})`
      : `Model: ${provider}`;
  }
  if (configured)
    return `Model: ${configured} (operator configured; provider id unavailable)`;
  if (served?.model?.trim()) return `Model: ${served.model.trim()}`;
  return "Model identity unavailable";
}
