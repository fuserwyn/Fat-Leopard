function daysWordForm(count: number): string {
  return count === 1 ? "day" : "days";
}

/** Remaining time until kick — mirrors formatDurationToDays on backend. */
export function formatDurationToDays(ms: number): string {
  const totalMinutes = Math.max(0, Math.floor(ms / 60_000));
  const days = Math.floor(totalMinutes / (24 * 60));
  const hours = Math.floor((totalMinutes % (24 * 60)) / 60);
  const minutes = totalMinutes % 60;

  if (days > 0) {
    if (hours > 0) return `${days} ${daysWordForm(days)} ${hours} hr`;
    return `${days} ${daysWordForm(days)}`;
  }
  if (hours > 0) {
    if (minutes > 0) return `${hours} hr ${minutes} min`;
    return `${hours} hr`;
  }
  return `${minutes} min`;
}

export type RemovalRemaining = {
  ms: number;
  text: string;
  deadlineLocal: string;
};

/** Warning milestone day (5/6/7) from timer. null — too early. */
export function inactiveDaysFromRemovalRemaining(ms: number): number | null {
  const hours = ms / 3_600_000;
  if (hours > 72) return null;
  if (hours > 48) return 5;
  if (hours > 24) return 6;
  return 7;
}

/** Time left until inactivity_removal_at (RFC3339 from backend). */
export function removalRemainingUntil(removalAtRFC3339: string, now: Date): RemovalRemaining | null {
  const deadline = new Date(removalAtRFC3339);
  if (Number.isNaN(deadline.getTime())) return null;
  const ms = deadline.getTime() - now.getTime();
  if (ms <= 0) return null;
  const pad = (n: number) => String(n).padStart(2, "0");
  const deadlineLocal = `${pad(deadline.getMonth() + 1)}/${pad(deadline.getDate())}/${deadline.getFullYear()}, ${pad(deadline.getHours())}:${pad(deadline.getMinutes())}`;
  return { ms, text: formatDurationToDays(ms), deadlineLocal };
}
