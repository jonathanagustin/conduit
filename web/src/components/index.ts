export { AppLogo, type AppLogoProps } from "./AppLogo/AppLogo.js";
export { AppHeader, type AppHeaderProps } from "./AppHeader/AppHeader.js";
export { AppFrame, type AppFrameProps } from "./AppFrame/AppFrame.js";
export { AccessibleNonIdealState } from "./AccessibleNonIdealState.js";
export {
  NavigationRail,
  type NavigationRailProps,
  type RailDestination,
} from "./NavigationRail/NavigationRail.js";
export { QueryState, type QueryStateProps } from "./QueryState.js";
export { Timestamp, countOf } from "./Timestamp.js";
export { ObjectHeader, type ObjectHeaderProps, type ResourceAction } from "./ObjectHeader/ObjectHeader.js";
// Presentation shared by more than one feature. Each of these was reached by a
// cross-feature import before stage 2 of docs/ui/foundry-frontend-architecture.md
// moved it here; rule 5 of scripts/verify-console-boundaries.mjs now stops that
// class of import coming back.
export {
  HEALTH_INTENT,
  ROLLOUT_INTENT,
  CONFIG_INTENT,
  LIFECYCLE_INTENT,
  HealthTag,
  RolloutTag,
  ConfigTag,
  LifecycleTag,
} from "./statusTags.js";
export { PLAN_INTENT, PlanStatusTag } from "./PlanStatusTag.js";
export { ReleaseChannelGraph, latestForChannel, orderChannels } from "./ReleaseChannelGraph.js";
export { TransitionDetail } from "./TransitionDetail.js";
