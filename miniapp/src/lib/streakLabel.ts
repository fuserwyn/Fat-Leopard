import type { UILang } from "./i18n";

/** Russian plural form index: 0 — one (1, 21), 1 — few (2–4), 2 — many (0, 5–20…). */
function ruPluralForm(n: number): 0 | 1 | 2 {
  const abs = Math.abs(Math.trunc(n));
  const mod100 = abs % 100;
  const mod10 = abs % 10;
  if (mod100 >= 11 && mod100 <= 14) return 2;
  if (mod10 === 1) return 0;
  if (mod10 >= 2 && mod10 <= 4) return 1;
  return 2;
}

/** День / дня / дней; day / days (integer ≥ 0). */
export function daysWord(n: number, lang: UILang = "ru"): string {
  if (lang === "en") return n === 1 ? "day" : "days";
  return (["день", "дня", "дней"] as const)[ruPluralForm(n)];
}

/** Lang-aware alias (default ru). */
export function daysWordRu(n: number, lang: UILang = "ru"): string {
  return daysWord(n, lang);
}

/** For aria / pack feed streak badge hints. */
export function streakStreakAriaLabel(days: number, lang: UILang = "ru"): string {
  if (lang === "en") {
    if (days <= 0) return "Streak: no consecutive workout days yet";
    return `Streak: ${days} ${daysWord(days, "en")} in a row`;
  }
  if (days <= 0) return "Стрик: пока нет дней тренировок подряд";
  return `Стрик: ${days} ${daysWord(days, "ru")} подряд`;
}

/** Local midnight for YYYY-MM-DD. */
export function parseLocalDateYMD(ymd: string): Date {
  const [y, m, d] = ymd.split("-").map(Number);
  return new Date(y, m - 1, d);
}

/** Days since last workout from YYYY-MM-DD and local “today”. */
export function daysSinceLastTrainingFromDate(
  lastTrainingDate: string | null | undefined,
  now: Date = new Date(),
): number {
  const ltd = lastTrainingDate?.trim();
  if (!ltd) return -1;
  const last = parseLocalDateYMD(ltd);
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = Math.round((today.getTime() - last.getTime()) / 86_400_000);
  return Math.max(0, diff);
}

/** Midnight after catch-up day: end of calendar day after lastTrainingDate. */
export function streakBurnDeadline(lastTrainingDate: string): Date {
  const last = parseLocalDateYMD(lastTrainingDate.trim());
  const deadline = new Date(last.getFullYear(), last.getMonth(), last.getDate());
  deadline.setDate(deadline.getDate() + 2);
  return deadline;
}

/**
 * Milliseconds until streak expires.
 *
 * Streak is alive while the last workout was today (`daysSinceLastTraining === 0`)
 * or yesterday (`=== 1`). Expires at midnight after the catch-up day — see streakBurnDeadline.
 * If `lastTrainingDate` (YYYY-MM-DD) is passed, use it (more accurate on UI ticks without API refresh).
 *
 * Returns null if no streak, date unknown, or deadline passed.
 */
export function streakBurnRemainingMs(
  streak: number,
  daysSinceLastTraining: number,
  now: Date = new Date(),
  lastTrainingDate?: string | null,
): number | null {
  if (streak <= 0) return null;
  const ltd = lastTrainingDate?.trim();
  if (ltd) {
    const ms = streakBurnDeadline(ltd).getTime() - now.getTime();
    return ms > 0 ? ms : null;
  }
  if (daysSinceLastTraining < 0 || daysSinceLastTraining > 1) return null;
  const deadline = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  deadline.setDate(deadline.getDate() + (2 - daysSinceLastTraining));
  const ms = deadline.getTime() - now.getTime();
  return ms > 0 ? ms : null;
}

/** Compact countdown: “5h 20m”, “1d 4h”, “18m” (ru: “5ч 20м”, “1д 4ч”, “18м”). */
export function formatStreakBurnRemaining(ms: number, lang: UILang = "en"): string {
  const totalMin = Math.max(0, Math.floor(ms / 60_000));
  const days = Math.floor(totalMin / (60 * 24));
  const hours = Math.floor((totalMin % (60 * 24)) / 60);
  const mins = totalMin % 60;
  const u = lang === "ru" ? { d: "д", h: "ч", m: "м" } : { d: "d", h: "h", m: "m" };
  if (days > 0) return `${days}${u.d} ${hours}${u.h}`;
  if (hours > 0) return `${hours}${u.h} ${mins}${u.m}`;
  return `${mins}${u.m}`;
}

/**
 * Aligns API days_since_last_training with local recompute from lastTrainingDate.
 * If raw date is much larger than backend value — sick freeze, trust API.
 */
function resolveStreakIdleDays(
  daysSinceLastTraining: number,
  now: Date,
  lastTrainingDate?: string | null,
): { days: number; frozen: boolean } {
  const ltd = lastTrainingDate?.trim();
  const daysFromDate = ltd ? daysSinceLastTrainingFromDate(ltd, now) : -1;

  if (daysSinceLastTraining >= 0) {
    const frozen = daysFromDate >= 0 && daysFromDate > daysSinceLastTraining + 1;
    if (frozen) {
      return { days: daysSinceLastTraining, frozen: true };
    }
    const days =
      daysFromDate >= 0 ? Math.max(daysFromDate, daysSinceLastTraining) : daysSinceLastTraining;
    return { days, frozen: false };
  }

  if (daysFromDate >= 0) {
    return { days: daysFromDate, frozen: false };
  }
  return { days: daysSinceLastTraining, frozen: false };
}

/**
 * Effective streak for UI: 0 after midnight on the day after last workout
 * if no report on that catch-up day (see EffectiveStreakDays on backend).
 */
export function effectiveStreakDays(
  streak: number,
  daysSinceLastTraining: number,
  now: Date = new Date(),
  lastTrainingDate?: string | null,
): number {
  if (streak <= 0) return 0;
  const { days, frozen } = resolveStreakIdleDays(daysSinceLastTraining, now, lastTrainingDate);
  if (days < 0) return streak;
  if (days >= 2) return 0;
  if (frozen) return streak;
  const ms = streakBurnRemainingMs(streak, days, now, lastTrainingDate);
  if (ms == null) return 0;
  return streak;
}

/** Caption under streak counter, or null if not burning. */
export function streakBurnLabel(
  streak: number,
  daysSinceLastTraining: number,
  now: Date = new Date(),
  lastTrainingDate?: string | null,
  lang: UILang = "ru",
): string | null {
  const { days, frozen } = resolveStreakIdleDays(daysSinceLastTraining, now, lastTrainingDate);
  if (frozen || days >= 2) return null;
  const ms = streakBurnRemainingMs(streak, days, now, lastTrainingDate);
  if (ms == null) return null;
  const left = formatStreakBurnRemaining(ms, lang);
  return lang === "en" ? `streak resets in ${left}` : `стрик сгорит через ${left}`;
}

const STREAK_SAVE_HINTS: Record<UILang, Record<string, string>> = {
  en: {
    ok: "Restore a lost streak if you forgot to log a workout.",
    no_attempts: "No saves left — level up to earn more.",
    not_needed: "Streak still alive — log a workout first; save isn’t needed.",
    too_late: "More than one day missed — one save can’t fix that.",
    no_training_history: "No workouts yet — nothing to save.",
  },
  ru: {
    ok: "Верни потерянный стрик, если забыл записать тренировку.",
    no_attempts: "Спасений не осталось — повышай уровень, чтобы получить ещё.",
    not_needed: "Стрик ещё жив — сначала запиши тренировку, спасение не нужно.",
    too_late: "Пропущено больше одного дня — одно спасение этого не исправит.",
    no_training_history: "Тренировок пока нет — спасать нечего.",
  },
};

/** Hint for “Save streak” when button is unavailable or risky. */
export function streakSaveHint(
  daysSinceLastTraining: number,
  attemptsAvail: number,
  lang: UILang = "ru",
): string | null {
  const reason = streakSaveWindowError(daysSinceLastTraining, attemptsAvail);
  const hints = STREAK_SAVE_HINTS[lang];
  return hints[reason ?? "ok"] ?? null;
}

/**
 * Save window: exactly 2 calendar days from last_training_date (streak already 0 in UI),
 * with saves available. Matches UseStreakSaveAttemptForAPI on backend.
 */
export function streakSaveWindowError(
  daysSinceLastTraining: number,
  attemptsAvail: number,
): "no_attempts" | "no_training_history" | "not_needed" | "too_late" | null {
  if (attemptsAvail <= 0) return "no_attempts";
  if (daysSinceLastTraining < 0) return "no_training_history";
  if (daysSinceLastTraining < 2) return "not_needed";
  if (daysSinceLastTraining > 2) return "too_late";
  return null;
}

export function canUseStreakSave(daysSinceLastTraining: number, attemptsAvail: number): boolean {
  return streakSaveWindowError(daysSinceLastTraining, attemptsAvail) === null;
}

/** “cup” / “cups” (ru: кубок / кубка / кубков) for progress counter. */
export function cupsWord(n: number, lang: UILang = "ru"): string {
  if (lang === "en") return n === 1 ? "cup" : "cups";
  return (["кубок", "кубка", "кубков"] as const)[ruPluralForm(n)];
}

/** Lang-aware alias (default ru). */
export function cupsWordRu(n: number, lang: UILang = "ru"): string {
  return cupsWord(n, lang);
}
