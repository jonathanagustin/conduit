/** One rendering of a moment in time, used everywhere.
 *
 * The plan's accessibility contract requires both forms: "Dates show relative
 * and exact UTC values; user locale/timezone presentation never changes stored
 * values." Relative is what an operator reads; exact UTC is what they compare
 * across screens and paste into a ticket. Rendering only one of them, or
 * stripping the Z as an earlier version of the Plans table did, lets two pages
 * disagree about the order of events. */
export function Timestamp({ value }: { value: string | undefined }) {
  if (value === undefined || value === "") {
    return <>—</>;
  }
  const at = new Date(value);
  if (Number.isNaN(at.getTime())) {
    // An unparseable timestamp is shown as sent rather than as "Invalid Date":
    // the raw value is the only thing that can be reported to whoever sent it.
    return <span title="This timestamp could not be parsed">{value}</span>;
  }
  const exact = at.toISOString().replace(".000", "");
  return (
    <time dateTime={exact} title={exact}>
      {relative(at)}
    </time>
  );
}

/** Exported so it can be tested and mutated directly. It was private, which
 * left the whole of this file's arithmetic unreachable from the unit suite and
 * showed up as 51 uncovered mutants -- coverage the browser tests do exercise
 * but Stryker cannot see.
 *
 * Deliberately coarse. This is orientation, not arithmetic -- the exact value
 * is one hover away, and a false precision like "3 minutes 12 seconds ago"
 * invites an operator to reason about a number that is already stale. */
export function relative(at: Date, now: number = Date.now()): string {
  const seconds = Math.round((now - at.getTime()) / 1000);
  const future = seconds < 0;
  const magnitude = Math.abs(seconds);
  const [amount, unit] =
    magnitude < 60
      ? [magnitude, "second"]
      : magnitude < 3600
        ? [Math.round(magnitude / 60), "minute"]
        : magnitude < 86_400
          ? [Math.round(magnitude / 3600), "hour"]
          : [Math.round(magnitude / 86_400), "day"];
  const plural = amount === 1 ? unit : `${unit}s`;
  return future ? `in ${amount} ${plural}` : `${amount} ${plural} ago`;
}

/** Subject-verb agreement for counted nouns. The Overview said "1 installation
 * have not reported", which is the first sentence an operator reads. */
export function countOf(count: number, singular: string, plural?: string): string {
  return `${count} ${count === 1 ? singular : (plural ?? `${singular}s`)}`;
}
