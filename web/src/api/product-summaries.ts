import { create } from "@bufbuild/protobuf";
import { createClient, type Transport } from "@connectrpc/connect";
import {
  ListProductSummariesRequestSchema,
  ProductReadService,
  type ListProductSummariesRequest,
} from "./gen/conduit/catalog/v1/product_summary_pb.js";
import { hubConnectError } from "./hub-connect.js";
import { HubError } from "./hub-error.js";
export { hubConnectTransport as productSummariesTransport } from "./hub-connect.js";

type Filters = Partial<
  Pick<ListProductSummariesRequest, "limit" | "cursor" | "search">
>;

/** One authorized native page, retaining opaque cursors and exact int64 counts. */
export async function fetchProductSummaries(
  transport: Transport,
  signal?: AbortSignal,
  filters: Filters = {},
) {
  try {
    const page = await createClient(
      ProductReadService,
      transport,
    ).listProductSummaries(create(ListProductSummariesRequestSchema, filters), {
      signal,
      timeoutMs: 10000,
    });
    if (
      page.pageSize !== page.products.length ||
      page.total < BigInt(page.products.length) ||
      page.limit < 1 ||
      page.limit > 200 ||
      page.products.length > page.limit ||
      page.products.some(
        (product) => !product.product || product.releaseCount < BigInt(0),
      )
    )
      throw new HubError(502, "Product summary index is unavailable.");
    return page;
  } catch (error) {
    if (error instanceof HubError) throw error;
    throw hubConnectError(error);
  }
}
