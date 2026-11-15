import {
  ONTOLOGY,
  type LinkTargets,
  type ObjectTypeName,
  type ObjectTypes,
} from "./generated.js";
import type { Transport } from "./transport.js";

export type Where =
  | { type: "eq"; field: string; value: string }
  | { type: "and" | "or"; clauses: Where[] }
  | { type: "not"; clauses: [Where] };

export type WireObjectSet =
  | { type: "base"; objectType: string }
  | { type: "filter"; objectSet: WireObjectSet; where: Where }
  | { type: "searchAround"; objectSet: WireObjectSet; link: string };

export interface PageOptions {
  pageSize?: number;
  pageToken?: string;
  select?: string[];
}

export interface Page<T> {
  data: T[];
  nextPageToken?: string;
  totalCount?: number;
}

type FieldFilter<T extends ObjectTypeName> = Partial<
  Record<keyof ObjectTypes[T] & string, string>
>;
type LinkTarget<
  T extends ObjectTypeName,
  L extends keyof LinkTargets[T],
> = Extract<LinkTargets[T][L], ObjectTypeName>;

const LOAD_OBJECTS = `/api/v2/ontologies/${ONTOLOGY}/objectSets/loadObjects`;

function isWhere(value: object): value is Where {
  const fields = value as Record<string, unknown>;
  if (fields.type === "eq") return "field" in fields;
  if (fields.type === "and" || fields.type === "or" || fields.type === "not") {
    return Array.isArray(fields.clauses);
  }
  return false;
}

/** Turns `{ a: "x", b: "y" }` into eq clauses joined with and. */
function fromFields(
  fields: Record<string, string | undefined>,
): Where | undefined {
  const clauses: Where[] = [];
  for (const field of Object.keys(fields).sort()) {
    const value = fields[field];
    if (value !== undefined) clauses.push({ type: "eq", field, value });
  }
  if (clauses.length === 0) return undefined;
  if (clauses.length === 1) return clauses[0];
  return { type: "and", clauses };
}

/** An immutable description of a set of objects. Each method returns a new set. */
export class ObjectSet<T extends ObjectTypeName> {
  private readonly transport: Transport;
  private readonly wire: WireObjectSet;

  constructor(transport: Transport, wire: WireObjectSet) {
    this.transport = transport;
    this.wire = wire;
  }

  static of<T extends ObjectTypeName>(
    transport: Transport,
    objectType: T,
  ): ObjectSet<T> {
    return new ObjectSet<T>(transport, { type: "base", objectType });
  }

  where(filter: FieldFilter<T> | Where): ObjectSet<T> {
    const where = isWhere(filter)
      ? filter
      : fromFields(filter as Record<string, string | undefined>);
    if (where === undefined) return this;
    return new ObjectSet<T>(this.transport, {
      type: "filter",
      objectSet: this.wire,
      where,
    });
  }

  pivotTo<L extends keyof LinkTargets[T] & string>(
    link: L,
  ): ObjectSet<LinkTarget<T, L>> {
    return new ObjectSet<LinkTarget<T, L>>(this.transport, {
      type: "searchAround",
      objectSet: this.wire,
      link,
    });
  }

  async fetchPage(options: PageOptions = {}): Promise<Page<ObjectTypes[T]>> {
    const response = (await this.transport(LOAD_OBJECTS, {
      method: "POST",
      body: {
        objectSet: this.wire,
        pageSize: options.pageSize,
        pageToken: options.pageToken,
        select: options.select,
      },
    })) as
      | { data?: ObjectTypes[T][]; nextPageToken?: string; totalCount?: number }
      | undefined;

    const page: Page<ObjectTypes[T]> = { data: response?.data ?? [] };
    if (response?.nextPageToken) page.nextPageToken = response.nextPageToken;
    if (response?.totalCount !== undefined)
      page.totalCount = response.totalCount;
    return page;
  }

  async *asyncIter(
    options: Omit<PageOptions, "pageToken"> = {},
  ): AsyncGenerator<ObjectTypes[T]> {
    let pageToken: string | undefined;
    do {
      const page = await this.fetchPage(
        pageToken ? { ...options, pageToken } : options,
      );
      yield* page.data;
      pageToken = page.nextPageToken;
    } while (pageToken);
  }

  async all(limit = 1000): Promise<ObjectTypes[T][]> {
    const objects: ObjectTypes[T][] = [];
    if (limit <= 0) return objects;
    for await (const object of this.asyncIter()) {
      objects.push(object);
      if (objects.length >= limit) break;
    }
    return objects;
  }

  async fetchOne(primaryKey: string): Promise<ObjectTypes[T] | undefined> {
    const page = await this.where({
      type: "eq",
      field: "__primaryKey",
      value: primaryKey,
    }).fetchPage({
      pageSize: 1,
    });
    return page.data[0];
  }

  toWire(): WireObjectSet {
    return this.wire;
  }
}
