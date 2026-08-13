import type { RailDestination } from "../components/index.js";

/** The rail follows the plan's information architecture rather than a guess at
 * what an operator uses most:
 *
 *   Home / overview, Search, Environments, Software Catalog, Applications
 *   { Change Requests, Fleet (upstream Entity Map role), Export, Import,
 *     Risk Management }, Notifications, Settings
 *
 * An earlier version promoted Fleet, Plans and Promotions to rail peers. That
 * is not the documented architecture, and the rail is how the console teaches
 * the shape of the product: a destination on the rail reads as a top-level
 * thing, so promoting Plans made Plans look like a peer of Environments rather
 * than something Environments produce.
 *
 * Two deliberate departures, both because the alternative would be a rail entry
 * that does nothing:
 *
 *   - Search is absent. The plan puts it on the rail, but there is no /v1/search
 *     contract and the plan itself says to use page-local filtering "until a
 *     server search contract exists" rather than silently downloading every
 *     resource. A rail button opening an empty omnibar would promise a
 *     capability the Hub does not have.
 *   - Notifications and Settings are absent for the same reason: the plan marks
 *     Notifications as "external integration until implemented", and there is no
 *     settings surface yet.
 *
 * Plans and Promotions sit under Applications rather than being dropped. They
 * are reachable from Environment and Product detail, which is where the evidence
 * puts them, and the Applications entries are a shortcut rather than their home.
 */
export const RAIL_DESTINATIONS: RailDestination[] = [
  { id: "overview", label: "Home", icon: "home", to: "/overview" },
  { id: "environments", label: "Environments", icon: "cloud", to: "/environments" },
  { id: "products", label: "Software Catalog", icon: "cube", to: "/products" },
];

export const APPLICATION_DESTINATIONS: RailDestination[] = [
  { id: "change-requests", label: "Change Requests", icon: "inbox", to: "/change-requests" },
  { id: "fleet", label: "Fleet", icon: "grid-view", to: "/fleet" },
  { id: "plans", label: "Plans", icon: "gantt-chart", to: "/plans" },
  { id: "promotions", label: "Promotions", icon: "flow-end", to: "/promotions" },
  { id: "export", label: "Export", icon: "export", to: "/export" },
  { id: "import", label: "Import", icon: "import", to: "/import" },
  { id: "risk", label: "Risk Management", icon: "shield", to: "/risk" },
  { id: "teams", label: "Teams", icon: "people", to: "/teams" },
];
