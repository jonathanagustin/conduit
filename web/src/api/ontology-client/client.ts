import { createActions, type Actions } from "./actions.js";
import { ONTOLOGY, type ObjectTypeName } from "./generated.js";
import { ObjectSet } from "./objectSet.js";
import { fetchTransport, type Transport } from "./transport.js";

export interface ConduitClient {
  objects<T extends ObjectTypeName>(type: T): ObjectSet<T>;
  actions: Actions;
  metadata(): Promise<unknown>;
}

export function createConduitClient(
  opts: { baseUrl?: string; transport?: Transport } = {},
): ConduitClient {
  const transport = opts.transport ?? fetchTransport(opts.baseUrl);
  return {
    objects: (type) => ObjectSet.of(transport, type),
    actions: createActions(transport),
    metadata: () => transport(`/api/v2/ontologies/${ONTOLOGY}/fullMetadata`),
  };
}
