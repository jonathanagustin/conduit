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
  checkpointFrequency?: number;
  selections: BundleSelectionRule[];
  resourceTypes?: string[];
  includeFutureResourceTypes?: boolean;
  requireSecurityPass?: boolean;
  includeArtifacts?: boolean;
  requireMalwarePass?: boolean;
  buildDespiteMissingResources?: boolean;
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
