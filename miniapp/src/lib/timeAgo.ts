import { daysWordRu } from "./streakLabel";

/** Rough relative time from now. */
export function timeAgoFromISO(iso: string, now = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "—";
  const s = Math.max(0, Math.floor((now - t) / 1000));
  if (s < 60) return "just now";
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} hr ago`;
  const d = Math.floor(h / 24);
  if (d < 7) return `${d} ${daysWordRu(d)} ago`;
  return new Date(t).toLocaleString("en-US", { day: "2-digit", month: "2-digit" });
}

/**
 * Local date and time for feed and chats (device timezone).
 * Accepts ISO string or unix millis.
 */
export function formatChatTime(input: string | number): string {
  const t = typeof input === "number" ? input : new Date(input).getTime();
  if (Number.isNaN(t)) return "";
  const d = new Date(t);
  const date = d.toLocaleDateString("en-US", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  });
  const time = d.toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit" });
  return `${date}, ${time}`;
}

/** Same as formatChatTime — for feed and cards. */
export const formatLocalDateTime = formatChatTime;
