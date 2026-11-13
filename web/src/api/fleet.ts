import { useQuery } from "@tanstack/react-query";

import { getJSON } from "./client.js";
import { hasInstant, type FleetInstallation, type FleetProjection, type FleetQuery } from "./types.js";

export function useFleet(query: FleetQuery) {
  return useQuery({
    queryKey: ["fleet", query],
    queryFn: ({ signal }) =>
      getJSON<FleetProjection>("/fleet/installations", {
        signal,
        query: { ...query },
      }),
    // The fleet view is the one screen an operator leaves open, so it polls.
    // Thirty seconds is well inside the Agent reporting interval and does not
    // make the Hub's authorization filter the hot path.
    refetchInterval: 30_000,
  });
}

/** Where a row's facts came from.
 *
 * `BuildFleet` (internal/reporting/domain/fleet.go:322) merges two sides into
 * one table and the merge is invisible in the result. A desired step with no
 * report becomes a row with `Health=UNKNOWN, Rollout=OUT_OF_DATE,
 * Config=INDETERMINATE, Lifecycle=UNKNOWN, Stale=true` (`:326-332`) -- four
 * status values that are *placeholders*, not observations. A report with no
 * desired step becomes a row too (`:338-340`), and that is the opposite
 * problem: something is running that nothing declared.
 *
 * Reading those two apart used to require knowing the Go. An operator seeing
 * `UNKNOWN / OUT_OF_DATE` could not tell "the Agent has not reported yet" from
 * "the Agent reported and the news is bad". */
export type InstallationSource = "DECLARED_AND_REPORTED" | "DECLARED_ONLY" | "REPORTED_ONLY";

export function installationSource(item: FleetInstallation): InstallationSource {
  // hasInstant, not a presence check: observedAt is a Go time.Time marked
  // omitempty, and encoding/json does not omit structs, so an unreported row
  // carries "0001-01-01T00:00:00Z" rather than nothing at all.
  const reported = hasInstant(item.observedAt) || (item.observedVersion ?? "") !== "";
  const declared = (item.desiredVersion ?? "") !== "";
  if (reported && declared) return "DECLARED_AND_REPORTED";
  return reported ? "REPORTED_ONLY" : "DECLARED_ONLY";
}

/** What the tag says, and why an operator should care. Kept beside the
 * derivation so the two cannot drift into disagreeing. */
export function installationSourceLabel(source: InstallationSource): {
  label: string;
  intent: "success" | "warning" | "none";
  title: string;
} {
  switch (source) {
    case "DECLARED_AND_REPORTED":
      return {
        label: "reported",
        intent: "success",
        title: "Declared in desired state and reported by an Agent.",
      };
    case "DECLARED_ONLY":
      return {
        label: "declared",
        intent: "none",
        title:
          "Declared in desired state; no Agent has reported it. Its health, rollout, config and lifecycle are placeholders, not observations.",
      };
    case "REPORTED_ONLY":
      return {
        label: "unmanaged",
        intent: "warning",
        title: "An Agent reports this installation, but no desired state declares it.",
      };
  }
}

/** A fleet RID as `installationRID` mints it
 * (internal/controlplane/governance.go:1388): `installation/<environment>/<id>`.
 *
 * The Hub's own fleet search matches installationId, environment and product
 * (internal/reporting/domain/fleet.go:443-448) and knows nothing about RIDs, so
 * pasting one -- which is exactly what an operator does with an identifier from
 * an audit entry or a ticket -- matched nothing at all. Splitting it here turns
 * that into the environment filter plus the installation id the Hub does
 * understand, with no Hub change.
 *
 * Deliberately strict about the shape: an installation id may not contain a
 * slash, so a third separator means this is not an installation RID and the
 * term is left alone rather than silently truncated. */
export function parseInstallationRID(
  term: string,
): { environment: string; installationId: string } | undefined {
  const parts = term.trim().split("/");
  if (parts.length !== 3 || parts[0] !== "installation") {
    return undefined;
  }
  const [, environment, installationId] = parts;
  if (environment === undefined || installationId === undefined) {
    return undefined;
  }
  if (environment === "" || installationId === "") {
    return undefined;
  }
  return { environment, installationId };
}
