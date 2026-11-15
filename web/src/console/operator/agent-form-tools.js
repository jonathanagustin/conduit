import { z } from "zod";
import schemas from "./agent-form-tools.json";

export const formToolOffers = () =>
  Object.entries(schemas).map(([name, parameters]) => ({
    name,
    description:
      "Suggest a planning draft change; never submit answers or approve delivery.",
    parameters: JSON.parse(JSON.stringify(parameters)),
  }));
const validators = new Map(
  Object.entries(schemas).map(([name, schema]) => [
    name,
    z.fromJSONSchema(schema),
  ]),
);
const refused = (code) => ({ ok: false, code });

// This executor owns only visible drafts/focus and a human confirmation prompt.
// A call ID ledger prevents repeated effects after snapshot recovery in this tab.
export class FormToolExecutor {
  constructor() {
    this.results = new Map();
  }
  async execute(call, offered, binding) {
    const fingerprint = JSON.stringify([call.name, call.arguments]);
    const previous = this.results.get(call.id);
    if (previous)
      return previous.fingerprint === fingerprint
        ? previous.result
        : refused("CALL_ID_CONFLICT");
    // Reserve before awaiting a confirmation, so even concurrent recovery cannot
    // show a second prompt for this call.
    const result = this.apply(call, offered, binding);
    this.results.set(call.id, { fingerprint, result });
    return result;
  }
  async apply(call, offered, binding) {
    const offer = offered.find((item) => item.name === call.name);
    const canonical = validators.get(call.name);
    if (!offer || !canonical) return refused("TOOL_NOT_OFFERED");
    let args;
    try {
      if (typeof call.arguments !== "string" || call.arguments.length > 8192)
        return refused("INVALID_ARGUMENTS");
      args = JSON.parse(call.arguments);
      if (
        !canonical.safeParse(args).success ||
        !z.fromJSONSchema(offer.parameters).safeParse(args).success
      )
        return refused("INVALID_ARGUMENTS");
    } catch {
      return refused("INVALID_ARGUMENTS");
    }
    if (call.name === "request_confirmation") {
      return {
        ok: true,
        confirmed: Boolean(await binding.confirm(args.message)),
      };
    }
    const field = binding.fields().get(args.field);
    if (!field) return refused("UNKNOWN_FIELD");
    if (call.name === "focus_field") {
      binding.focus(args.field);
      return { ok: true };
    }
    if (field.readOnly) return refused("READ_ONLY_FIELD");
    const value = call.name === "clear_field" ? "" : args.value;
    if (
      call.name === "fill_field" &&
      ((field.type === "boolean"
        ? typeof value !== "boolean"
        : typeof value !== "string") ||
        (field.choices?.length && !field.choices.includes(value)))
    )
      return refused("INVALID_FIELD_VALUE");
    // Human drafts, including deliberately cleared values, always win.
    if (binding.dirty(args.field)) return refused("HUMAN_DRAFT_PRESENT");
    binding.set(args.field, String(value));
    return { ok: true };
  }
}
