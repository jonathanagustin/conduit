import { useQuery } from "@tanstack/react-query";

import { getJSON } from "./client.js";
import type { FleetProjection, FleetQuery } from "./types.js";

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
