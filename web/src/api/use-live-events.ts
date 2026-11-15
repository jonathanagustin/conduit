import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useSyncExternalStore,
} from "react";
import {
  idleLiveStatus,
  LiveSubscription,
  type LiveOptions,
} from "./live-events.js";

// A React adapter over the same cursor/transport lifecycle used by the active
// operator read model. This hook owns no application/model cache.
export function useLiveEvents(identity: string, options: LiveOptions) {
  const callbacks = useRef(options);
  callbacks.current = options;
  const client = useMemo(
    () =>
      new LiveSubscription({
        ...(options.fetch ? { fetch: options.fetch } : {}),
        catchUp: (context) => callbacks.current.catchUp(context),
        invalidate: (events, context) =>
          callbacks.current.invalidate(events, context),
      }),
    [options.fetch],
  );
  const status = useSyncExternalStore(
    client.subscribe,
    client.getSnapshot,
    () => idleLiveStatus,
  );
  useEffect(() => {
    client.start(identity);
    return () => client.stop();
  }, [client, identity]);
  const retry = useCallback(() => client.retry(), [client]);
  return { status, retry };
}
