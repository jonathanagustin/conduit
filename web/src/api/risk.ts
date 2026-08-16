import { useQuery } from "@tanstack/react-query";

import { getJSON } from "./client.js";

/** Risk transport. GET /v1/catalog-retirements returns the recalls that
 * withdraw Release ranges from selection, as one named array
 * ({"retirements": [...]}). Fields are the catalog.Recall Go struct's. A
 * recall's source decides whether a risk comparison may ever override it, so it
 * is load-bearing, not decoration: MANUAL recalls are never second-guessed by a
 * scanner. */

export interface RecallStrategy {
  type: string;
  minimumVersion?: string;
  freezeUntil?: string;
  environments?: string[];
}

export interface Recall {
  id: string;
  product: string;
  startingVersion: string;
  endingVersionExclusive?: string;
  excludedVersions?: string[];
  strategy: RecallStrategy;
  reason: string;
  reverted?: boolean;
  singleVersion?: boolean;
  source?: string;
  assessmentRunId?: string;
}

export function useRetirements() {
  return useQuery({
    queryKey: ["catalog-retirements"],
    queryFn: ({ signal }) =>
      getJSON<{ retirements: Recall[] }>("/catalog-retirements", { signal }).then(
        (body) => body.retirements,
      ),
  });
}

/** The affected range, in the form an operator reads. A single-version recall
 * is one version; an open-ended one is "from X"; a bounded one names the
 * exclusive ceiling, because the difference between inclusive and exclusive is
 * exactly the kind of off-by-one that strands or frees the wrong Release. */
export function recallRange(recall: Recall): string {
  if (recall.singleVersion === true) {
    return recall.startingVersion;
  }
  if (recall.endingVersionExclusive !== undefined && recall.endingVersionExclusive !== "") {
    return `${recall.startingVersion} up to (excluding) ${recall.endingVersionExclusive}`;
  }
  return `${recall.startingVersion} and later`;
}
