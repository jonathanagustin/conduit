import { readFileSync } from "node:fs";
import { describe, expect, it } from "vite-plus/test";
import {
  ontologyCatalog,
  objectKey,
  validationParameters,
} from "../../console/operator/ontology-explorer.js";
import { projectMetadata } from "../../console/operator/ontology-graph.js";
const fixture = JSON.parse(
  readFileSync(
    new URL(
      "../../../../testdata/ontology/full-metadata.json",
      import.meta.url,
    ),
    "utf8",
  ),
);
describe("native ontology views", () => {
  it("projects the checked-in catalog without inventing empty or missing metadata", () => {
    const catalog = ontologyCatalog(fixture);
    expect(catalog.types.map((type) => type.apiName)).toContain("Environment");
    expect(catalog.actions.map((action) => action.apiName)).toContain(
      "createChangeRequest",
    );
    expect(ontologyCatalog({ objectTypes: {} }).types).toEqual([]);
    expect(() => ontologyCatalog({})).toThrow();
    expect(() => ontologyCatalog({ objectTypes: [] })).toThrow();
    expect(projectMetadata({ objectTypes: {} })).toEqual({
      types: [],
      edges: [],
    });
    expect(() => projectMetadata({})).toThrow();
  });
  it("draws only unambiguous shared link identifiers", () => {
    const entry = (apiName: string, rid?: string) => ({
      objectType: { apiName, properties: {} },
      linkTypes: [{ apiName: "related", linkTypeRid: rid }],
    });
    expect(
      projectMetadata({
        objectTypes: { a: entry("a", "r"), b: entry("b", "r") },
      }).edges,
    ).toHaveLength(1);
    expect(
      projectMetadata({
        objectTypes: {
          a: entry("a", "r"),
          b: entry("b", "r"),
          c: entry("c", "r"),
        },
      }).edges,
    ).toEqual([]);
    expect(
      projectMetadata({ objectTypes: { a: entry("a"), b: entry("b") } }).edges,
    ).toEqual([]);
  });
  it("preserves zero keys and constrains validation to bounded parameter objects", () => {
    expect(
      objectKey({ primaryKey: "id" }, { id: 0, __primaryKey: "other" }),
    ).toBe("0");
    expect(objectKey({ primaryKey: "id" }, { id: { secret: "x" } })).toBe("");
    expect(validationParameters('{"id":"a"}')).toEqual({ id: "a" });
    for (const text of [
      "[]",
      "null",
      "true",
      '"value"',
      "{",
      " ".repeat(65537),
    ])
      expect(() => validationParameters(text)).toThrow();
  });
});
