export const AREAS = {
  "area-command": {
    label: "Command Center",
    tabs: [
      { id: "command", label: "Overview", screen: "cc-overview" },
      {
        id: "example",
        label: "Example fleet",
        screen: "s13-fleet-overview",
      },
      {
        id: "tower",
        label: "Release Control Tower",
        screen: "s15-plan-detail",
      },
    ],
  },
  "area-packaging": {
    label: "Packaging",
    tabs: [
      { id: "runs", label: "Runs", screen: "pkg-runs" },
      { id: "timeline", label: "Timeline", screen: "s08-staged-run" },
      { id: "pipeline", label: "Pipeline", screen: "s07-pipeline" },
      { id: "workspace", label: "Workspace", screen: "s02-workspaces" },
      { id: "workspace-source", label: "Source", screen: "s09-source" },
      { id: "workspace-preview", label: "Preview", screen: "s11-preview" },
      {
        id: "workspace-lifecycle",
        label: "Lifecycle",
        screen: "s12-lifecycle",
      },
      { id: "bundle", label: "Bundle", screen: "rm08-bundle-preview" },
      { id: "proposals", label: "Proposals", screen: "pt01-proposal-trace" },
    ],
  },
  "area-releases": {
    label: "Releases",
    tabs: [
      { id: "environments", label: "Environments", screen: "s14-env-detail" },
      { id: "products", label: "Products", screen: "rel-products" },
      {
        id: "dependencies",
        label: "Dependencies",
        screen: "rel-dependencies",
      },
      { id: "plans", label: "Plans", screen: "rm03-plan-detail" },
      { id: "channels", label: "Channels", screen: "rm-channels" },
      { id: "waves", label: "Waves", screen: "rm-waves" },
      { id: "windows", label: "Windows", screen: "rm09-fleet-scheduler" },
      { id: "planning", label: "Planning", screen: "s15-plan-detail" },
      { id: "ramp", label: "Ramp", screen: "rm10-ramped-rollout" },
      {
        id: "convergence",
        label: "Convergence",
        screen: "rm05-release-rollout",
      },
      { id: "activity", label: "Activity", screen: "rm02-env-activity" },
      { id: "promotion", label: "Promotion", screen: "rm06-promotion-pipe" },
      { id: "recall", label: "Recall", screen: "rm09-recalls" },
    ],
  },
  "area-changes": {
    label: "Change requests",
    tabs: [
      { id: "list", label: "Requests", screen: "rm07-change-requests" },
      { id: "approvals", label: "Approvals", screen: "s16-approvals-board" },
      { id: "diff", label: "Diff", screen: "s10-diff" },
      { id: "gate", label: "Constraint gate", screen: "rm04-constraints" },
    ],
  },
  "area-intelligence": {
    label: "Intelligence",
    tabs: [
      { id: "logic", label: "Logic", screen: "ai01-logic" },
      { id: "evals", label: "Evals", screen: "ai02-evals" },
      { id: "courses", label: "Courses", screen: "ai04-courses" },
      { id: "events", label: "Automations", screen: "ai03-events" },
    ],
  },
  "area-fleet": {
    label: "Fleet",
    tabs: [
      {
        id: "environments",
        label: "Environments",
        screen: "flt-environments",
      },
      { id: "drift", label: "Drift", screen: "s14-env-detail" },
      { id: "spokes", label: "Spoke operations", screen: "s17-spokes-audit" },
    ],
  },
  "area-audit": {
    label: "Audit",
    tabs: [
      { id: "chain", label: "Hash chain", screen: "au01-audit" },
      {
        id: "status",
        label: "Platform status",
        screen: "adm-platform-status",
      },
    ],
  },
};
