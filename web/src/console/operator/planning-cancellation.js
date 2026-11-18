export const planningOwnerKey = (store) =>
  JSON.stringify([
    store.reviewOperatorActor || "",
    store.reviewOperatorIdentities || [],
  ]);

export async function cancelPlanningRun(sessionId, runId, fetchImpl = fetch) {
  const response = await fetchImpl(
    `/v1/planning-sessions/${encodeURIComponent(sessionId)}/turns/${encodeURIComponent(runId)}/cancel`,
    {
      method: "POST",
      headers: {
        "content-type": "application/json",
        accept: "application/json",
      },
      body: "{}",
    },
  );
  let body;
  try {
    body = await response.json();
  } catch {
    throw new Error("The Hub did not return a cancellation acknowledgement.");
  }
  if (!response.ok)
    throw new Error(
      body?.error || `Cancellation unavailable (${response.status}).`,
    );
  if (
    body?.runId !== runId ||
    body?.valueTier !== "CONDUIT_NATIVE" ||
    !["cancellation_requested", "cancelled", "completed", "failed"].includes(
      body?.state,
    )
  )
    throw new Error(
      "The Hub returned an invalid cancellation acknowledgement.",
    );
  return body;
}

export async function requestPlanningCancellation(
  store,
  selection,
  fetchImpl,
  rerender,
) {
  const sameSelection = () =>
    store.planning?.session?.id === selection.sessionId &&
    planningOwnerKey(store) === selection.owner;
  if (!sameSelection()) return;
  const pending = { ...selection, status: "loading" };
  store.planning = { ...store.planning, cancellation: pending };
  rerender();
  const current = () =>
    sameSelection() && store.planning.cancellation === pending;
  if (!current()) return;
  try {
    const receipt = await cancelPlanningRun(
      selection.sessionId,
      selection.runId,
      fetchImpl,
    );
    if (!current()) return;
    store.planning = {
      ...store.planning,
      cancellation: { ...pending, status: "ready", receipt },
    };
  } catch (error) {
    if (!current()) return;
    store.planning = {
      ...store.planning,
      cancellation: {
        ...pending,
        status: "failed",
        error: String(
          error?.message || "Cancellation acknowledgement unavailable.",
        ),
      },
    };
  }
  rerender();
}
