/**
 * Imported first by the console entry, for its effect.
 *
 * Modules are evaluated in import order, so installing here -- rather than in
 * the entry's own body, which runs after every import -- means no module can
 * have captured the unwrapped `fetch` first. A transport built at module scope
 * would otherwise send its writes without the CSRF header and be refused.
 */
import { installHubSession } from "./hub-session";

installHubSession(window);
