// @conduit/ontology-client: typed objects, links, and governed actions over the Hub ontology API.

export { createConduitClient } from "./client.js";
export type { ConduitClient } from "./client.js";
export { ObjectSet } from "./objectSet.js";
export type { Page, PageOptions, Where, WireObjectSet } from "./objectSet.js";
export { ACTION_RISK, createActions, riskOf } from "./actions.js";
export type {
  ActionOutcome,
  ActionParamsFor,
  ActionRisk,
  Actions,
} from "./actions.js";
export { OntologyError, fetchTransport } from "./transport.js";
export type { Transport } from "./transport.js";
export * from "./generated.js";
export {
  ConduitClientProvider,
  useAction,
  useConduitClient,
  useObject,
  useObjects,
} from "./hooks.js";
export type { QueryState } from "./hooks.js";
