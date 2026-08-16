import { useQuery } from "@tanstack/react-query";

import { getJSON } from "./client.js";

/** Import transport. GET /v1/bundle-imports returns the record of every offline
 * Bundle imported into an Environment, as one named array ({"imports": [...]}).
 * Fields are the controlplane.BundleImport Go struct's. Each import carries its
 * per-stage tasks, so the detail can show where an import got to or failed
 * without a second request. Tasks contain only resource coordinates and
 * redacted diagnostics -- never artifact bodies or credentials -- so surfacing
 * them is safe. */

export interface BundleImportTask {
  sequence: number;
  stage: string;
  status: string;
  resources?: string[];
  failure?: string;
  startedAt?: string;
  finishedAt?: string;
}

export interface BundleImport {
  id: string;
  bundleId: string;
  manifestDigest: string;
  environment: string;
  changeRequestId?: string;
  requestedBy: string;
  status: string;
  error?: string;
  tasks: BundleImportTask[];
  requestedAt: string;
  importedAt?: string;
  revision: number;
}

export function useBundleImports() {
  return useQuery({
    queryKey: ["bundle-imports"],
    queryFn: ({ signal }) =>
      getJSON<{ imports: BundleImport[] }>("/bundle-imports", { signal }).then((body) => body.imports),
    refetchInterval: 30_000,
  });
}

/** The Blueprint intent for a Bundle import or task status. The vocabulary is
 * the Hub's, read from controlplane.bundles.go, not guessed: an import is
 * PENDING_APPROVAL, then ADMITTED or ERROR; a task is PENDING, SUCCEEDED,
 * FAILED, or SKIPPED. ADMITTED and SUCCEEDED are the done-and-good states, ERROR
 * and FAILED the bad ones, PENDING_APPROVAL is the one awaiting a person, and
 * anything still moving or abandoned is left neutral so it reads as neither an
 * error nor a done deal. An unknown status also falls through to neutral rather
 * than being coloured on a guess. */
export function statusIntent(status: string): "success" | "danger" | "warning" | "none" {
  switch (status.toUpperCase()) {
    case "ADMITTED":
    case "SUCCEEDED":
      return "success";
    case "ERROR":
    case "FAILED":
      return "danger";
    case "PENDING_APPROVAL":
      return "warning";
    default:
      return "none";
  }
}
