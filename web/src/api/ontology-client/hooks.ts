import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import type { ActionOutcome, ActionParamsFor } from "./actions.js";
import { createConduitClient, type ConduitClient } from "./client.js";
import type { ObjectTypeName, ObjectTypes } from "./generated.js";
import type { ObjectSet, PageOptions } from "./objectSet.js";

const ClientContext = createContext<ConduitClient | null>(null);

/** Provides one client to the hooks below. Without it they use a default client. */
export function ConduitClientProvider(props: {
  client?: ConduitClient;
  children?: ReactNode;
}) {
  const client = useMemo(
    () => props.client ?? createConduitClient(),
    [props.client],
  );
  return createElement(
    ClientContext.Provider,
    { value: client },
    props.children,
  );
}

let fallback: ConduitClient | null = null;

export function useConduitClient(): ConduitClient {
  const client = useContext(ClientContext);
  if (client) return client;
  fallback ??= createConduitClient();
  return fallback;
}

export type QueryState<T> =
  | { status: "loading"; data?: undefined; error?: undefined }
  | { status: "ready"; data: T; error?: undefined }
  | { status: "error"; data?: undefined; error: Error };

/**
 * Loads one page of an object set. `build` picks the set from the client;
 * `key` must change whenever the set does, since sets are compared by key.
 */
export function useObjects<T extends ObjectTypeName>(
  key: string,
  build: (client: ConduitClient) => ObjectSet<T>,
  options: PageOptions = {},
): QueryState<ObjectTypes[T][]> & { reload: () => void } {
  const client = useConduitClient();
  const [state, setState] = useState<QueryState<ObjectTypes[T][]>>({
    status: "loading",
  });
  const [generation, setGeneration] = useState(0);
  const optionsKey = JSON.stringify(options);

  useEffect(() => {
    let live = true;
    setState({ status: "loading" });
    build(client)
      .fetchPage(JSON.parse(optionsKey) as PageOptions)
      .then(
        (page) => live && setState({ status: "ready", data: page.data }),
        (error: unknown) =>
          live &&
          setState({
            status: "error",
            error: error instanceof Error ? error : new Error(String(error)),
          }),
      );
    return () => {
      live = false;
    };
    // `build` is intentionally keyed by `key`.
  }, [client, key, optionsKey, generation]);

  const reload = useCallback(() => setGeneration((n) => n + 1), []);
  return { ...state, reload };
}

/** Loads one object by primary key. */
export function useObject<T extends ObjectTypeName>(
  type: T,
  primaryKey: string | undefined,
): QueryState<ObjectTypes[T] | undefined> {
  const client = useConduitClient();
  const [state, setState] = useState<QueryState<ObjectTypes[T] | undefined>>({
    status: "loading",
  });
  useEffect(() => {
    let live = true;
    if (!primaryKey) {
      setState({ status: "ready", data: undefined });
      return;
    }
    setState({ status: "loading" });
    client
      .objects(type)
      .fetchOne(primaryKey)
      .then(
        (data) => live && setState({ status: "ready", data }),
        (error: unknown) =>
          live &&
          setState({
            status: "error",
            error: error instanceof Error ? error : new Error(String(error)),
          }),
      );
    return () => {
      live = false;
    };
  }, [client, type, primaryKey]);
  return state;
}

/**
 * Runs a governed action. `validate` never executes; `apply` follows the
 * risk rules in actions.ts, so a low-risk action needs `confirmed: true` and
 * a high-risk one only ever becomes a change request.
 */
export function useAction<N extends string>(name: N) {
  const client = useConduitClient();
  const [pending, setPending] = useState(false);
  const [outcome, setOutcome] = useState<ActionOutcome | null>(null);
  const [error, setError] = useState<Error | null>(null);

  const run = useCallback(
    async (step: () => Promise<ActionOutcome>) => {
      setPending(true);
      setError(null);
      try {
        const result = await step();
        setOutcome(result);
        return result;
      } catch (caught) {
        const failure =
          caught instanceof Error ? caught : new Error(String(caught));
        setError(failure);
        throw failure;
      } finally {
        setPending(false);
      }
    },
    [],
  );

  const validate = useCallback(
    (params: ActionParamsFor<N>) =>
      run(() => client.actions.validate(name, params)),
    [client, name, run],
  );
  const apply = useCallback(
    (params: ActionParamsFor<N>, opts?: { confirmed?: boolean }) =>
      run(() => client.actions.apply(name, params, opts)),
    [client, name, run],
  );
  return { validate, apply, pending, outcome, error };
}
