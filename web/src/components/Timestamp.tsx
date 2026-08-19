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

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;
/** Foundry's own coarse month and year, not calendar arithmetic. Its ladder
 * divides a whole-day span by these constants, so "2 months ago" means 60 days
 * and not "the same date two months back". Reproduced rather than corrected:
 * the point is to read the way the product reads. */
const MONTH = 30 * DAY;
const YEAR = 365 * DAY;

/** Exported so it can be tested and mutated directly. It was private, which
 * left the whole of this file's arithmetic unreachable from the unit suite and
 * showed up as 51 uncovered mutants -- coverage the browser tests do exercise
 * but Stryker cannot see.
 *
 * The ladder is Foundry's, recovered from the captured `apollo-control-panel`
 * 6.533.15 bundle rather than guessed -- see
 * `docs/apollo/FRONTEND-CAS-CAPTURE.md` for the capture and the exact source
 * expressions. The shape it fixes: an earlier hand-rolled version stopped at
 * days, so a release cut last quarter read "97 days ago", and it rounded rather
 * than truncated, so 59m59s read "60 minutes ago" -- an hour that had not
 * happened yet.
 *
 * Two deliberate departures from the captured original, both because this
 * console pins UTC where Foundry follows the viewer's zone and locale:
 *
 *   - the calendar-day comparison behind "Yesterday"/"Tomorrow" is made in UTC,
 *     so the boundary is the same one the exact value in the tooltip shows;
 *   - `{formattedTime}` renders as 24-hour `HH:MM UTC` instead of Foundry's
 *     locale-formatted `{hour, minute}`, for the same reason and because a
 *     locale-dependent string cannot be pinned by a test.
 *
 * Still deliberately coarse. This is orientation, not arithmetic -- the exact
 * value is one hover away, and a false precision like "3 minutes 12 seconds
 * ago" invites an operator to reason about a number that is already stale. */
export function relative(at: Date, now: number = Date.now()): string {
  const delta = at.getTime() - now;
  const elapsed = Math.abs(delta);
  // Under a second in either direction is not worth a number. Foundry answers
  // "now" here, before it decides which side of the clock the value is on.
  if (elapsed < SECOND) {
    return "now";
  }
  const ahead = delta > 0;
  if (elapsed < MINUTE) {
    return signed(Math.floor(elapsed / SECOND), "second", ahead);
  }
  if (elapsed < HOUR) {
    return signed(Math.floor(elapsed / MINUTE), "minute", ahead);
  }
  // The named days are tested before the hour count, so 23:50 read at 00:10 is
  // "Yesterday at 23:50 UTC" rather than "1 hour ago". The elapsed time is the
  // same; which day it happened on is the thing an operator is reconciling
  // against a ticket.
  const days = utcDayOffset(at, now);
  if (days === -1) {
    return `Yesterday at ${utcTime(at)}`;
  }
  if (days === 1) {
    return `Tomorrow at ${utcTime(at)}`;
  }
  if (elapsed < DAY || days === 0) {
    return signed(Math.floor(elapsed / HOUR), "hour", ahead);
  }
  return coarse(Math.abs(days), ahead);
}

/** Beyond a day Foundry counts whole calendar days, not elapsed milliseconds,
 * and then divides that span down. Keeping the day count as the input is what
 * makes the units stable: a value does not slide from "3 weeks ago" to "2 weeks
 * ago" because the clock crossed noon. */
function coarse(days: number, ahead: boolean): string {
  const span = days * DAY;
  if (span < 3 * WEEK) {
    return signed(days, "day", ahead);
  }
  if (span < 2 * MONTH) {
    return signed(Math.floor(span / WEEK), "week", ahead);
  }
  if (span < 2 * YEAR) {
    return signed(Math.floor(span / MONTH), "month", ahead);
  }
  return signed(Math.floor(span / YEAR), "year", ahead);
}

/** Hub and browser clocks disagree; "-30 seconds ago" would be nonsense. */
function signed(amount: number, unit: string, ahead: boolean): string {
  const counted = countOf(amount, unit);
  return ahead ? `in ${counted}` : `${counted} ago`;
}

/** Whole days between the two moments' UTC calendar dates: negative in the
 * past, positive in the future, zero for the same UTC day. */
function utcDayOffset(at: Date, now: number): number {
  return Math.round((utcMidnight(at) - utcMidnight(new Date(now))) / DAY);
}

function utcMidnight(at: Date): number {
  return Date.UTC(at.getUTCFullYear(), at.getUTCMonth(), at.getUTCDate());
}

function utcTime(at: Date): string {
  const hours = String(at.getUTCHours()).padStart(2, "0");
  const minutes = String(at.getUTCMinutes()).padStart(2, "0");
  return `${hours}:${minutes} UTC`;
}

/** Subject-verb agreement for counted nouns. The Overview said "1 installation
 * have not reported", which is the first sentence an operator reads. */
export function countOf(count: number, singular: string, plural?: string): string {
  return `${count} ${count === 1 ? singular : (plural ?? `${singular}s`)}`;
}
