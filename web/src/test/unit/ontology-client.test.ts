import { readFileSync } from "node:fs";
import { describe, expect, it } from "vite-plus/test";

import {
  createConduitClient,
  OBJECT_TYPES,
  type Transport,
} from "../../api/ontology-client/index.js";
import { generate } from "../../../../scripts/generate-ontology-client.mjs";

type Call = { path: string; body: any };

function recording(responses: unknown[]): {
  transport: Transport;
  calls: Call[];
} {
  const calls: Call[] = [];
  const transport: Transport = async (path, init) => {
    calls.push({ path, body: init?.body });
    return responses.shift();
  };
  return { transport, calls };
}

const fixture = JSON.parse(
  readFileSync(
    new URL(
      "../../../../testdata/ontology/full-metadata.json",
      import.meta.url,
    ),
    "utf8",
  ),
);

describe("generated ontology types", () => {
  it("match the checked-in metadata", () => {
    const onDisk = readFileSync(
      new URL("../../api/ontology-client/generated.ts", import.meta.url),
      "utf8",
    );
    expect(generate(fixture)).toBe(onDisk);
  });

  it("resolve link targets across both sides of a link", () => {
    expect(OBJECT_TYPES.Environment.links.workProfile.target).toBe(
      "WorkProfile",
    );
    expect(OBJECT_TYPES.Plan.links.artifacts.target).toBe("Artifact");
    expect(OBJECT_TYPES.Spoke.links.environments.target).toBe("Environment");
  });
});

describe("object sets", () => {
  it("filters, pivots, and pages through loadObjects", async () => {
    const { transport, calls } = recording([
      {
        data: [{ __apiName: "Plan", __primaryKey: "p1" }],
        nextPageToken: "t2",
        totalCount: 2,
      },
      { data: [{ __apiName: "Plan", __primaryKey: "p2" }], totalCount: 2 },
    ]);
    const client = createConduitClient({ transport });
    const plans = await client
      .objects("Environment")
      .where({ id: "lab" })
      .pivotTo("plans")
      .all();

    expect(plans.map((plan) => plan.__primaryKey)).toEqual(["p1", "p2"]);
    expect(calls[0]!.path).toBe(
      "/api/v2/ontologies/conduit/objectSets/loadObjects",
    );
    expect(calls[0]!.body.objectSet).toEqual({
      type: "searchAround",
      link: "plans",
      objectSet: {
        type: "filter",
        objectSet: { type: "base", objectType: "Environment" },
        where: { type: "eq", field: "id", value: "lab" },
      },
    });
    expect(calls[1]!.body.pageToken).toBe("t2");
  });

  it("fetches one object by primary key", async () => {
    const { transport, calls } = recording([
      { data: [{ __apiName: "Spoke", __primaryKey: "s1" }] },
    ]);
    const spoke = await createConduitClient({ transport })
      .objects("Spoke")
      .fetchOne("s1");
    expect(spoke?.__primaryKey).toBe("s1");
    expect(calls[0]!.body.pageSize).toBe(1);
    expect(calls[0]!.body.objectSet.where).toEqual({
      type: "eq",
      field: "__primaryKey",
      value: "s1",
    });
  });
});

const valid = { validation: { result: "VALID", parameters: {} } };

describe("governed actions", () => {
  it("asks for confirmation before running a low-risk action", async () => {
    const { transport, calls } = recording([valid]);
    const outcome = await createConduitClient({ transport }).actions.apply(
      "fork",
      { resource: "wp-1" },
    );
    expect(outcome).toEqual({ kind: "confirm", action: "fork", risk: "low" });
    expect(calls).toHaveLength(1);
    expect(calls[0]!.body.options.mode).toBe("VALIDATE_ONLY");
  });

  it("runs a confirmed low-risk action", async () => {
    const { transport, calls } = recording([valid]);
    const outcome = await createConduitClient({ transport }).actions.apply(
      "fork",
      { resource: "wp-1" },
      { confirmed: true },
    );
    expect(outcome).toEqual({ kind: "applied", action: "fork" });
    expect(calls[0]!.body.options.mode).toBe("VALIDATE_AND_EXECUTE");
  });

  it("turns a high-risk action into a change request, even when confirmed", async () => {
    const { transport, calls } = recording([
      {
        ...valid,
        edits: {
          type: "edits",
          edits: [
            {
              type: "addObject",
              objectType: "ChangeRequest",
              primaryKey: "CR-9",
            },
          ],
        },
      },
    ]);
    const outcome = await createConduitClient({ transport }).actions.apply(
      "delete",
      { environment: "lab", targetKind: "workProfile", targetId: "wp-1" },
      { confirmed: true },
    );
    expect(outcome).toEqual({
      kind: "change-request",
      action: "delete",
      changeRequestId: "CR-9",
      stored: true,
    });
    expect(calls[0]!.path).toBe(
      "/api/v2/ontologies/conduit/actions/createChangeRequest/apply",
    );
    expect(calls[0]!.body.parameters.changeType).toBe("delete");
  });

  it("reports parameter and submission failures", async () => {
    const { transport } = recording([
      {
        validation: {
          result: "INVALID",
          parameters: {
            environment: {
              result: "INVALID",
              evaluatedConstraints: [
                { result: "INVALID", message: "unknown environment" },
              ],
            },
            targetId: { result: "VALID" },
          },
          submissionCriteria: [],
        },
      },
    ]);
    const outcome = await createConduitClient({
      transport,
    }).actions.validate("createChangeRequest", {
      environment: "nope",
      targetKind: "plan",
      targetId: "p1",
      changeType: "deploy",
    });
    expect(outcome).toEqual({
      kind: "invalid",
      messages: { environment: "unknown environment" },
    });
  });

  it("treats unknown actions as high risk", async () => {
    const { transport, calls } = recording([valid]);
    const outcome = await createConduitClient({ transport }).actions.validate(
      "somethingNew",
      {},
    );
    expect(outcome.kind).toBe("change-request");
    expect(calls[0]!.body.options.mode).toBe("VALIDATE_ONLY");
  });
});
