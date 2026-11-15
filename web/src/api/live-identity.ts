// This local partition key comes from the existing authorized session read.
// It is never sent as stream identity/authority or persisted in browser storage.
export function liveSessionIdentity(value: unknown): string {
  if (!value || typeof value !== "object") return "";
  const session = value as Record<string, unknown>;
  const person = (value: unknown) => {
    if (!value || typeof value !== "object") return null;
    const row = value as Record<string, unknown>;
    if (typeof row.username !== "string" || !row.username.trim()) return null;
    return [
      row.username,
      row.serviceAccount === true,
      Array.isArray(row.teams)
        ? row.teams.filter((v): v is string => typeof v === "string").sort()
        : [],
      row.teamsTruncated === true,
    ];
  };
  const actor = person(session.actor),
    effective = person(session.effective);
  if (!actor || !effective) return "";
  const strings = (value: unknown) =>
    Array.isArray(value)
      ? value.filter((v): v is string => typeof v === "string").sort()
      : [];
  const resources = Array.isArray(session.resources)
    ? session.resources
        .map((value) => {
          const row = value as Record<string, unknown> | null;
          return [
            typeof row?.resourceRid === "string" ? row.resourceRid : "",
            strings(row?.operations),
          ];
        })
        .sort((a, b) => String(a[0]).localeCompare(String(b[0])))
    : [];
  const key = JSON.stringify([
    actor,
    effective,
    session.delegated === true,
    strings(session.globalOperations),
    resources,
  ]);
  return key.length <= 65536 ? key : "";
}
