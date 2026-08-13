import { Button } from "@blueprintjs/core";

import type { PageMetadata } from "../api/types.js";
import "./index-pagination.css";

export function IndexPagination({
  page,
  total,
  testId,
  onMove,
}: {
  page: PageMetadata;
  total: number;
  testId: string;
  onMove: (cursor: string | undefined) => void;
}) {
  return (
    <nav className="console-index-pagination" aria-label="Result pages" data-testid={testId}>
      <span>
        Showing {page.pageSize} of {total} matching results
      </span>
      <div>
        <Button
          text="Previous"
          disabled={!page.hasPrevious}
          onClick={() => onMove(page.previousCursor)}
        />
        <Button
          text="Next"
          disabled={page.nextCursor === undefined}
          onClick={() => onMove(page.nextCursor)}
        />
      </div>
    </nav>
  );
}
