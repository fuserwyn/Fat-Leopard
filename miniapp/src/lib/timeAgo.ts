import type { UILang } from "./i18n";
import { daysWord } from "./streakLabel";

function dateLocale(lang: UILang): string {
  return lang === "ru" ? "ru-RU" : "en-US";
}

/** Rough relative time from now. */
export function timeAgoFromISO(iso: string, now = Date.now(), lang: UILang = "en"): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "—";
  const s = Math.max(0, Math.floor((now - t) / 1000));
  const ru = lang === "ru";
  if (s < 60) return ru ? "только что" : "just now";
  const m = Math.floor(s / 60);
  if (m < 60) return ru ? `${m} мин назад` : `${m} min ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return ru ? `${h} ч назад` : `${h} hr ago`;
  const d = Math.floor(h / 24);
  if (d < 7) return ru ? `${d} ${daysWord(d, "ru")} назад` : `${d} ${daysWord(d, "en")} ago`;
  return new Date(t).toLocaleString(dateLocale(lang), { day: "2-digit", month: "2-digit" });
}

/**
 * Local date and time for feed and chats (device timezone).
 * Accepts ISO string or unix millis.
 */
export function formatChatTime(input: string | number, lang: UILang = "en"): string {
  const t = typeof input === "number" ? input : new Date(input).getTime();
  if (Number.isNaN(t)) return "";
  const d = new Date(t);
  const locale = dateLocale(lang);
  const date = d.toLocaleDateString(locale, {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  });
  const time = d.toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" });
  return `${date}, ${time}`;
}

/** Same as formatChatTime — for feed and cards. */
export const formatLocalDateTime = formatChatTime;
