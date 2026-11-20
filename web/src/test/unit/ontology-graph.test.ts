import { describe, expect, it, vi } from "vite-plus/test";
import {
  createOntologyGraph,
  projectMetadata,
} from "../../console/operator/ontology-graph.js";

describe("ontology-graph", () => {
  it("projects object types and shared link edges from catalog metadata", () => {
    const projected = projectMetadata({
      objectTypes: {
        Environment: {
          objectType: {
            apiName: "Environment",
            displayName: "Environment",
            primaryKey: "id",
            properties: { id: { dataType: { type: "string" } } },
          },
          linkTypes: [
            {
              apiName: "plans",
              linkTypeRid: "rid:env-plans",
              cardinality: "MANY",
            },
          ],
        },
        Plan: {
          objectType: {
            apiName: "Plan",
            displayName: "Plan",
            primaryKey: "id",
            properties: { id: { dataType: { type: "string" } } },
          },
          linkTypes: [
            {
              apiName: "environment",
              linkTypeRid: "rid:env-plans",
              cardinality: "ONE",
            },
          ],
        },
      },
    });
    expect(projected.types.map((t: { apiName: string }) => t.apiName)).toEqual([
      "Environment",
      "Plan",
    ]);
    expect(projected.edges).toEqual([
      { from: "Environment", to: "Plan", label: "plans" },
    ]);
  });

  it("renders Inspect and Ask AI row actions and invokes bound callbacks", () => {
    const onInspect = vi.fn();
    const onAskAI = vi.fn();
    const state: Record<string, any> = {
      hub: { loading: true },
      reviewOperatorActor: "operator@example.test",
      reviewOperatorIdentities: ["operator@example.test"],
    };
    const graph = createOntologyGraph({
      state,
      render: vi.fn(),
      onInspect,
      onAskAI,
    });
    graph.extendState();
    const target: Record<string, any> = {};
    graph.bind(target);

    const root = { innerHTML: "" } as HTMLElement;
    // First render records the active native identity while hub is still loading.
    graph.renderers["og01-ontology-graph"](root);

    // Seed loaded state for the same identity so render does not issue network requests.
    state.hub = { loading: false };
    state.graphLoaded = true;
    state.graphLive = true;
    state.graphSelected = "Environment";
    state.graphTypes = [
      {
        apiName: "Environment",
        displayName: "Environment",
        description: "Runtime environment",
        primaryKey: "id",
        properties: ["id", "connection"],
        links: [],
      },
    ];
    state.graphObjects = [{ id: "prod-west", connection: "connected" }];
    state.graphNote = "Native metadata · first 256 types";
    state.graphObjectNote =
      "First 25 authorized sample rows; not a total count.";

    graph.renderers["og01-ontology-graph"](root);

    expect(root.innerHTML).toContain("inspectOntologyGraphObject");
    expect(root.innerHTML).toContain("askOntologyGraphObject");
    expect(root.innerHTML).toContain("prod-west");

    target.inspectOntologyGraphObject("Environment", "prod-west");
    target.askOntologyGraphObject("Environment", "prod-west");
    expect(onInspect).toHaveBeenCalledWith("Environment", "prod-west");
    expect(onAskAI).toHaveBeenCalledWith("Environment", "prod-west");
  });
});
