/** Labeled stand-ins shown next to Hub rows. They are not records from the Hub. */
export const EXAMPLE = "Example";

export const EXAMPLE_PROFILES = [
  { id: "example-edge", label: "Example edge profile" },
  { id: "example-baseline", label: "Example baseline" },
] as const;

export const EXAMPLE_SPOKES = [
  { id: "example-spoke-west", label: "Example spoke west" },
  { id: "example-spoke-store", label: "Example store and forward" },
] as const;

export const EXAMPLE_ENVIRONMENTS = [
  { id: "example-edge-01", reported: true },
] as const;

export const EXAMPLE_SAVED_VIEWS = [
  { id: "needs-approval", label: "Example: needs approval", view: "plans" },
  { id: "staged-runs", label: "Example: staged runs", view: "packaging" },
] as const;
