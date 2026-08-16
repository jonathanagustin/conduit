import { useQuery } from "@tanstack/react-query";

import { getJSON } from "./client.js";

/** Export transport. GET /v1/export-pipelines returns the configured pipelines
 * that build offline Bundles, as one named array ({"pipelines": [...]}). Fields
 * are the controlplane.ExportPipeline Go struct's. A pipeline names what it
 * selects, how it builds (SNAPSHOT or INCREMENTAL), and which security gates a
 * Release must pass to enter a Bundle. There is no GET for the Bundles a
 * pipeline has produced -- only GET /v1/bundles/{id} for one already known -- so
 * this screen shows the pipelines, not a build history the Hub does not list. */

export interface BundleSelectionRule {
  type: string;
  ids: string[];
  releaseStrategy?: string;
  releaseChannels?: string[];
}

export interface ExportPipeline {
  id: string;
  displayName: string;
  buildStrategy: string;
  // status is ACTIVE, ARCHIVED, or DELETED (controlplane.bundles.go). It matters
  // because GET /v1/export-pipelines returns every pipeline, archived and
  // deleted included, so a screen that ignored it would show a retired pipeline
  // as if it still exported. An empty status is treated as ACTIVE, matching the
  // Hub's own `status == "" || status == ACTIVE` reading.
  status?: string;
  checkpointFrequency?: number;
  selections: BundleSelectionRule[];
  resourceTypes?: string[];
  includeFutureResourceTypes?: boolean;
  requireSecurityPass?: boolean;
  includeArtifacts?: boolean;
  requireMalwarePass?: boolean;
  buildDespiteMissingResources?: boolean;
}

/** The lifecycle state of a pipeline, normalized so an empty status reads as
 * ACTIVE the way the Hub reads it, and a Blueprint intent for it: a live
 * pipeline is success, an archived one is muted to warning, and a deleted one
 * is danger -- none of which a reader should have to infer from its absence. */
export function pipelineStatus(pipeline: ExportPipeline): string {
  return pipeline.status === undefined || pipeline.status === "" ? "ACTIVE" : pipeline.status;
}

export function pipelineStatusIntent(status: string): "success" | "warning" | "danger" | "none" {
  switch (status.toUpperCase()) {
    case "ACTIVE":
      return "success";
    case "ARCHIVED":
      return "warning";
    case "DELETED":
      return "danger";
    default:
      return "none";
  }
}

export function useExportPipelines() {
  return useQuery({
    queryKey: ["export-pipelines"],
    queryFn: ({ signal }) =>
      getJSON<{ pipelines: ExportPipeline[] }>("/export-pipelines", { signal }).then(
        (body) => body.pipelines,
      ),
  });
}

/** The security gates a pipeline enforces before a Release enters a Bundle, as
 * short labels. Shown positively -- what is required -- because an offline
 * Bundle is trusted at its destination on exactly these guarantees, and an
 * operator reads "requires a security pass" faster than the absence of it. */
export function pipelineGuarantees(pipeline: ExportPipeline): string[] {
  const gates: string[] = [];
  if (pipeline.requireSecurityPass === true) gates.push("security pass");
  if (pipeline.requireMalwarePass === true) gates.push("malware pass");
  if (pipeline.includeArtifacts === true) gates.push("artifacts included");
  return gates;
}
