/** Append a reviewed context prompt without replacing work or sending a turn. */
export function appendAssistantDraft(
  current: string,
  addition: unknown,
): string | null {
  if (typeof addition !== "string" || !addition.trim()) return null;
  const text = addition.trim();
  const next = current.endsWith(text)
    ? current
    : current
      ? `${current}\n\n${text}`
      : text;
  return next.length <= 4000 ? next : null;
}
