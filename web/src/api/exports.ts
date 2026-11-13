import { useMutation, useQuery } from "@tanstack/react-query";

import { getJSON, postJSON } from "./client.js";
import { ApiError } from "./errors.js";

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

/** A label a pipeline requires or excludes when selecting resources, scoped to
 * an optional set of resource types. It mirrors controlplane.BundleLabelRequirement;
 * `values` empty means the label's presence is what matters, not a specific value. */
export interface BundleLabelRequirement {
  resourceTypes: string[];
  name: string;
  values?: string[];
}

/** One Entity's version as a Target Hub last reported running it, so a
 * disconnected source Hub can build an incremental Bundle against what the
 * target actually has. Mirrors controlplane.TargetHubEntityVersion. */
export interface TargetHubEntityVersion {
  environment: string;
  installationId: string;
  product: string;
  version: string;
}

/** The Entity versions recorded for one Target Hub, and when. Mirrors
 * controlplane.TargetHubVersions. */
export interface TargetHubVersions {
  targetHub: string;
  entities: TargetHubEntityVersion[];
  recordedAt: string;
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
  requiredLabels?: BundleLabelRequirement[];
  excludedLabels?: BundleLabelRequirement[];
  targetHubVersions?: Record<string, TargetHubVersions>;
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

/** One thing a build did not do, on a build that otherwise succeeded, matching
 * `domain.Advisory` (internal/artifacts/domain/envelope.go). `kind` is a closed
 * set precisely so a reader can group and act on advisories; `message` alone is
 * prose, and prose has to be parsed back apart to be useful. */
export interface BundleAdvisory {
  kind: string;
  message: string;
  subjects?: string[];
}

/** What a build would do if it ran now, from
 * `controlplane.ExportPipelinePreview`. The point of a pre-build preview is to
 * find out before building, and an omission is exactly the kind of thing worth
 * finding out then -- which is why `advisories` rides on this and not on the
 * pipeline record. */
export interface ExportPipelinePreview {
  pipelineId: string;
  pipelineRevision: number;
  generatedAt: string;
  added?: string[];
  updated?: string[];
  removed?: string[];
  unchanged?: string[];
  securityOutcome: string;
  advisories?: BundleAdvisory[];
}

/** Run the pre-build preview for a pipeline.
 *
 * A mutation rather than a query, and behind an explicit action rather than
 * fired on render, for two independent reasons. The route is
 * `POST /v1/export-pipelines/{id}/preview` -- POST is not a verb to issue
 * because a panel opened -- and `PreviewExportPipeline` does real work:
 * `selectResources` walks the catalog and `ListBundles` reads the lineage. It
 * is nonetheless side-effect-free: it reads, computes and returns, and writes
 * nothing (internal/controlplane/bundles.go:490).
 *
 * Two refusals are ordinary states rather than errors, and the screen says so
 * instead of showing a failure: the Hub answers 409 when the pipeline is not
 * active, and when the pipeline's schedule sets `disablePreBuildPreview`. */
export function useExportPipelinePreview() {
  return useMutation({
    mutationFn: (pipelineId: string) =>
      postJSON<ExportPipelinePreview>(
        `/export-pipelines/${encodeURIComponent(pipelineId)}/preview`,
        undefined,
      ),
  });
}

/** Why the Hub refused a preview, in the operator's terms, or undefined when
 * the failure is not one of the two documented refusals and should be reported
 * as the failure it is. Both are 409 from
 * `PreviewExportPipeline`; the message distinguishes them. */
export function previewRefusal(error: Error | null): string | undefined {
  if (!(error instanceof ApiError) || error.status !== 409) {
    return undefined;
  }
  if (error.message.includes("pre-build preview is disabled")) {
    return "This pipeline's schedule disables the pre-build preview, so the Hub will not compute one.";
  }
  if (error.message.includes("not active")) {
    return "This pipeline is not active, so there is no build to preview.";
  }
  return undefined;
}
