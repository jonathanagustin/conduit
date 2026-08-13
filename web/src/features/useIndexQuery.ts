import { type FormEvent, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

import type { CatalogQuery } from "../api/types.js";

/** URL ownership shared by bounded catalog indexes. Draft search text becomes
 * authoritative only on submit, avoiding a Hub request for every keystroke. */
export function useIndexQuery() {
  const [searchParams, setSearchParams] = useSearchParams();
  const query = useMemo<CatalogQuery>(() => {
    const next: CatalogQuery = {};
    for (const key of ["search", "cursor"] as const) {
      const value = searchParams.get(key);
      if (value !== null && value !== "") next[key] = value;
    }
    return next;
  }, [searchParams]);
  const [search, setSearch] = useState(query.search ?? "");
  useEffect(() => setSearch(query.search ?? ""), [query.search]);

  const applySearch = (event: FormEvent) => {
    event.preventDefault();
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      const normalized = search.trim();
      if (normalized === "") next.delete("search");
      else next.set("search", normalized);
      next.delete("cursor");
      return next;
    });
  };
  const moveToCursor = (cursor: string | undefined) =>
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (cursor === undefined || cursor === "") next.delete("cursor");
      else next.set("cursor", cursor);
      return next;
    });

  return { query, search, setSearch, applySearch, moveToCursor };
}
