import { describe, expect, it } from "vitest";
import { FormSurfaces, CATALOG_ID } from "../../console/operator/a2ui-forms.js";
const binding = { "@path": "/answers/port" };
function form() {
  return {
    version: "1.0",
    createSurface: {
      surfaceId: "form-1",
      catalogId: CATALOG_ID,
      dataModel: { answers: { port: "" } },
      components: [
        { id: "root", component: "Column", children: ["port", "submit"] },
        {
          id: "port",
          component: "TextField",
          label: "Which port?",
          value: binding,
          "@checks": [
            {
              condition: { "@call": "required", args: { value: binding } },
              message: "Enter the port.",
            },
            {
              condition: {
                "@call": "pattern",
                args: { value: binding, pattern: "^[0-9]+$" },
              },
              message: "Use digits.",
            },
          ],
        },
        {
          id: "submit",
          component: "Button",
          label: "Submit answers",
          action: {
            event: {
              name: "submit_needs_input",
              context: { "question:port": binding },
            },
          },
        },
      ],
    },
  };
}
describe("approved A2UI form surfaces", () => {
  it("binds model updates and human edits, validates fields, then resolves action context", () => {
    const store = new FormSurfaces();
    store.accept(form());
    expect(store.action("form-1", "submit")).toMatchObject({
      errors: { port: "Enter the port." },
      message: null,
    });
    store.accept({
      version: "1.0",
      updateDataModel: {
        surfaceId: "form-1",
        path: "/answers/port",
        value: "8080",
      },
    });
    expect(store.render("form-1")).toContain('value="8080"');
    store.input("form-1", "port", "9090");
    const result = store.action("form-1", "submit");
    expect(result.errors).toEqual({});
    expect(result.message).toMatchObject({
      version: "1.0",
      action: {
        name: "submit_needs_input",
        surfaceId: "form-1",
        sourceComponentId: "submit",
        context: { "question:port": "9090" },
      },
    });
    expect(result.message!.action.timestamp).toMatch(/^\d{4}-\d\d-/u);
    expect(() =>
      store.accept({
        version: "1.0",
        updateDataModel: {
          surfaceId: "form-1",
          path: "/answers/port",
          value: "overwrite",
        },
      }),
    ).not.toThrow();
    expect(store.render("form-1")).toContain('value="9090"');
    expect(store.action("form-1", "submit")).toMatchObject({
      message: { action: { context: { "question:port": "9090" } } },
    });
  });
  it("escapes text, labels, values and validation messages", () => {
    const message = form();
    message.createSurface.components[1].label = '<img src=x onerror="evil()">';
    message.createSurface.dataModel.answers.port = "<script>evil()</script>";
    const store = new FormSurfaces();
    store.accept(message);
    store.action("form-1", "submit");
    const html = store.render("form-1");
    expect(html).not.toContain("<img");
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;img");
    expect(html).toContain("&lt;script&gt;");
    expect(html).toContain('aria-invalid="true"');
    expect(html).toContain("aria-describedby=");
  });
  it("rejects unknown components, functions, HTML/URL properties and unsafe paths", () => {
    const mutations = [
      (message: any) => {
        message.createSurface.components[1].component = "Script";
      },
      (message: any) => {
        message.createSurface.components[1]["@checks"][0].condition["@call"] =
          "execute";
      },
      (message: any) => {
        message.createSurface.components[1].html = "<b>bad</b>";
      },
      (message: any) => {
        message.createSurface.components[1].url = "javascript:evil()";
      },
      (message: any) => {
        message.createSurface.components[1].value["@path"] =
          "/__proto__/polluted";
      },
      (message: any) => {
        message.createSurface.components[1][
          "@checks"
        ][1].condition.args.pattern = "(a+)+$";
      },
      (message: any) => {
        message.createSurface.catalogId = "third-party:catalog";
      },
      (message: any) => {
        message.createSurface.components[0].children = ["root"];
      },
    ];
    for (const mutate of mutations) {
      const message = structuredClone(form());
      mutate(message);
      const store = new FormSurfaces();
      expect(() => store.accept(message)).toThrow("refused");
      expect(store.surfaces.size).toBe(0);
      expect(store.failures).toEqual([{ code: "FORM_REFUSED" }]);
    }
  });
  it("preserves old state on invalid updates and applies deletion explicitly", () => {
    const store = new FormSurfaces();
    store.accept(form());
    expect(() =>
      store.accept({
        version: "1.0",
        updateComponents: {
          surfaceId: "form-1",
          components: [
            { id: "root", component: "Column", children: ["missing"] },
          ],
        },
      }),
    ).toThrow();
    expect(store.render("form-1")).toContain("Which port?");
    expect(() => store.accept(form())).toThrow();
    store.accept({ version: "1.0", deleteSurface: { surfaceId: "form-1" } });
    expect(store.render("form-1")).toBe("");
  });
  it("shows returned field errors and blocks resubmission until that field changes", () => {
    const store = new FormSurfaces();
    store.accept(form());
    store.input("form-1", "port", "8080");
    store.accept({
      version: "1.0",
      updateComponents: {
        surfaceId: "form-1",
        components: [
          {
            ...form().createSurface.components[1],
            error: "Use the reviewed port.",
          },
        ],
      },
    });
    expect(store.render("form-1")).toContain("Use the reviewed port.");
    expect(store.action("form-1", "submit").message).toBeNull();
    store.input("form-1", "port", "9090");
    expect(
      store.action("form-1", "submit").message!.action.context["question:port"],
    ).toBe("9090");
    expect(store.render("form-1")).not.toContain("Use the reviewed port.");
  });
});
