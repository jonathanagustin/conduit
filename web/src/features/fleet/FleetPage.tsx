import { type FormEvent, useEffect, useMemo, useState } from "react";
import {
  Button,
  Callout,
  HTMLSelect,
  HTMLTable,
  InputGroup,
  Spinner,
} from "@blueprintjs/core";
import { useSearchParams } from "react-router-dom";

import { ApiError } from "../../api/errors.js";
import { AccessibleNonIdealState, ObjectHeader } from "../../components/index.js";
import { useFleet } from "../../api/fleet.js";
import { CONFIG, HEALTH, LIFECYCLE, ROLLOUT, type FleetQuery } from "../../api/types.js";
import { ConfigTag, HealthTag, LifecycleTag, RolloutTag } from "./statusTags.js";
import { SummaryBar } from "./SummaryBar.js";
import "./fleet-page.css";

export function FleetPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const query = useMemo<FleetQuery>(() => {
    const next: FleetQuery = {};
    for (const key of [
      "environment",
      "product",
      "health",
      "rolloutStatus",
      "configStatus",
      "lifecycle",
      "search",
      "cursor",
    ] as const) {
      const value = searchParams.get(key);
      if (value !== null && value !== "") next[key] = value;
    }
    return next;
  }, [searchParams]);

  // exactOptionalPropertyTypes distinguishes "absent" from "present and
  // undefined", and the query is serialised into a URL where that distinction
  // is real: an absent filter is no parameter at all. Clearing a filter has to
  // delete the key rather than set it to undefined.
  const setFilter = (key: keyof FleetQuery, value: string | undefined) =>
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (value === undefined) {
        next.delete(key);
      } else {
        next.set(key, value);
      }
      next.delete("cursor");
      return next;
    });
  const [search, setSearch] = useState(query.search ?? "");
  useEffect(() => setSearch(query.search ?? ""), [query.search]);
  const { data, error, isPending, isFetching, refetch } = useFleet(query);

  const applySearch = (event: FormEvent) => {
    event.preventDefault();
    setFilter("search", search.trim() === "" ? undefined : search.trim());
  };

  const moveToCursor = (cursor: string | undefined) =>
    setSearchParams((current) => {
      const next = new URLSearchParams(current);
      if (cursor === undefined || cursor === "") next.delete("cursor");
      else next.set("cursor", cursor);
      return next;
    });

  if (error !== null) {
    const api = error instanceof ApiError ? error : undefined;
    return (
      <AccessibleNonIdealState
        headingLevel={1}
        icon={api?.unauthenticated === true ? "lock" : "error"}
        title={api?.unauthenticated === true ? "Not signed in" : "The fleet could not be read"}
        description={error.message}
        action={<Button text="Try again" onClick={() => void refetch()} />}
      />
    );
  }

  if (isPending) {
    return (
      <AccessibleNonIdealState headingLevel={1} icon={<Spinner />} title="Reading the fleet" />
    );
  }

  return (
    <div className="console-fleet" data-testid="fleet-page">
      <ObjectHeader icon="applications" kind="Application" title="Fleet" />
      <SummaryBar summary={data.summary} />

      <div className="console-fleet__controls">
        <form onSubmit={applySearch} className="console-fleet__search" role="search">
          <InputGroup
            leftIcon="search"
            placeholder="Search Entity, Environment or Product"
            value={search}
            onValueChange={setSearch}
            aria-label="Search installations"
          />
          <Button type="submit" text="Search" />
        </form>
        <Filter
          label="Health"
          options={HEALTH}
          value={query.health}
          onChange={(value) => setFilter("health", value)}
        />
        <Filter
          label="Rollout"
          options={ROLLOUT}
          value={query.rolloutStatus}
          onChange={(value) => setFilter("rolloutStatus", value)}
        />
        <Filter
          label="Config"
          options={CONFIG}
          value={query.configStatus}
          onChange={(value) => setFilter("configStatus", value)}
        />
        <Filter
          label="Lifecycle"
          options={LIFECYCLE}
          value={query.lifecycle}
          onChange={(value) => setFilter("lifecycle", value)}
        />
        <Button icon="refresh" text="Refresh" loading={isFetching} onClick={() => void refetch()} />
      </div>

      {data.summary.stale > 0 ? (
        <Callout intent="warning" icon="time" className="console-fleet__stale">
          {data.summary.stale} of {data.summary.total} installations have not reported recently.
          Their observed columns describe the last report, not the Entity now.
        </Callout>
      ) : null}

      {data.installations.length === 0 ? (
        <AccessibleNonIdealState
          icon="search"
          title="No installations match"
          description={
            (query.search ?? "").trim() === ""
              ? "No installation matches these filters."
              : `No installation matches "${query.search}".`
          }
        />
      ) : (
        <HTMLTable className="console-fleet__table" interactive striped>
          <thead>
            <tr>
              <th scope="col">Environment</th>
              <th scope="col">Entity</th>
              <th scope="col">Product</th>
              <th scope="col">Desired</th>
              <th scope="col">Observed</th>
              <th scope="col">Health</th>
              <th scope="col">Rollout</th>
              <th scope="col">Config</th>
              <th scope="col">Lifecycle</th>
            </tr>
          </thead>
          <tbody>
            {data.installations.map((item) => (
              <tr key={`${item.environment} ${item.installationId}`}>
                <td>{item.environment}</td>
                <td>{item.installationId}</td>
                <td>{item.product}</td>
                <td>{item.desiredVersion ?? "—"}</td>
                <td>
                  {item.observedVersion ?? "—"}
                  {item.stale ? <span className="console-fleet__stale-marker"> (stale)</span> : null}
                </td>
                <td>
                  <HealthTag value={item.health} />
                </td>
                <td>
                  <RolloutTag value={item.rolloutStatus} />
                </td>
                <td>
                  <ConfigTag value={item.configStatus} />
                </td>
                <td>
                  <LifecycleTag value={item.lifecycle} />
                </td>
              </tr>
            ))}
          </tbody>
        </HTMLTable>
      )}

      <nav className="console-fleet__pagination" aria-label="Fleet pages" data-testid="fleet-pagination">
        <span>
          Showing {data.pageSize} of {data.summary.total} matching installations
        </span>
        <div>
          <Button
            text="Previous"
            disabled={!data.hasPrevious}
            onClick={() => moveToCursor(data.previousCursor)}
          />
          <Button
            text="Next"
            disabled={data.nextCursor === undefined}
            onClick={() => moveToCursor(data.nextCursor)}
          />
        </div>
      </nav>
    </div>
  );
}

function Filter({
  label,
  options,
  value,
  onChange,
}: {
  label: string;
  options: readonly string[];
  value: string | undefined;
  onChange: (value: string | undefined) => void;
}) {
  return (
    <HTMLSelect
      aria-label={label}
      value={value ?? ""}
      onChange={(event) =>
        onChange(event.currentTarget.value === "" ? undefined : event.currentTarget.value)
      }
    >
      <option value="">{label}: any</option>
      {options.map((option) => (
        <option key={option} value={option}>
          {option}
        </option>
      ))}
    </HTMLSelect>
  );
}
