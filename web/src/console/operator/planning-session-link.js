/*
 * The planning session's place in the URL. A session id rides in the hash as
 * `#s01-intake/<sessionId>`, so a reload or a pasted link finds the session
 * the Hub still holds instead of an empty composer.
 */
export const PLANNING_SCREEN = "s01-intake";

const SESSION_ID = /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/;

/** `#screen/param` -> { screen, param }. A hash with no param has "". */
export function parseScreenHash(hash) {
  const raw = String(hash || "").replace(/^#/, "");
  const cut = raw.indexOf("/");
  if (cut === -1) return { screen: raw, param: "" };
  let param = raw.slice(cut + 1);
  try {
    param = decodeURIComponent(param);
  } catch {
    param = "";
  }
  return { screen: raw.slice(0, cut), param };
}

/** The session id a hash links to, or "" when it links to none. */
export function planningSessionFromHash(hash) {
  const { screen, param } = parseScreenHash(hash);
  return screen === PLANNING_SCREEN && SESSION_ID.test(param) ? param : "";
}

/** The hash for a planning session; the bare screen when there is none. */
export function planningHash(sessionId) {
  const id = String(sessionId || "");
  return SESSION_ID.test(id)
    ? `#${PLANNING_SCREEN}/${encodeURIComponent(id)}`
    : `#${PLANNING_SCREEN}`;
}

/** The hash a screen switch writes: the planning screen keeps its session. */
export function screenHash(screen, state) {
  return screen === PLANNING_SCREEN
    ? planningHash(state?.planning?.session?.id)
    : `#${screen}`;
}
