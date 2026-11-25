import type { UILang } from "./i18n";
import { daysWord } from "./streakLabel";

// Single source of truth for achievement thresholds: profile showcase and App toasts.
export const STREAK_ACHIEVEMENTS = [
  { days: 7, colorClass: "profile__achievement--7", variant: "paw" },
  { days: 14, colorClass: "profile__achievement--14", variant: "paw" },
  { days: 30, colorClass: "profile__achievement--30", variant: "paw" },
  { days: 42, colorClass: "profile__achievement--42", variant: "heart" },
  { days: 60, colorClass: "profile__achievement--60", variant: "paw" },
  { days: 90, colorClass: "profile__achievement--90", variant: "paw" },
  { days: 180, colorClass: "profile__achievement--180", variant: "paw" },
  { days: 365, colorClass: "profile__achievement--365", variant: "paw" },
  { days: 420, colorClass: "profile__achievement--420", variant: "paw-crown" },
  { days: 500, colorClass: "profile__achievement--500", variant: "paw" },
  { days: 1000, colorClass: "profile__achievement--1000", variant: "paw-crown" },
] as const;

export const WORKOUT_ACHIEVEMENTS = [
  { count: 1, variant: "paw" },
  { count: 10, variant: "paw" },
  { count: 20, variant: "paw" },
  { count: 42, variant: "heart" },
  { count: 50, variant: "paw" },
  { count: 100, variant: "paw" },
  { count: 200, variant: "paw" },
  { count: 420, variant: "crown" },
  { count: 500, variant: "paw" },
  { count: 1000, variant: "leo" },
] as const;

export function workoutsWordRu(n: number, lang: UILang = "en"): string {
  if (lang === "ru") {
    const mod100 = n % 100;
    const mod10 = n % 10;
    if (mod100 >= 11 && mod100 <= 14) return "тренировок";
    if (mod10 === 1) return "тренировка";
    if (mod10 >= 2 && mod10 <= 4) return "тренировки";
    return "тренировок";
  }
  return n === 1 ? "workout" : "workouts";
}

/** Achievement key: "streak-7", "workout-10". */
export type AchievementKey = string;

export function earnedAchievementKeys(achievementCount: number, workouts: number): AchievementKey[] {
  const keys: AchievementKey[] = [];
  const streakEarned = Math.max(0, Math.min(achievementCount, STREAK_ACHIEVEMENTS.length));
  for (let i = 0; i < streakEarned; i++) {
    keys.push(`streak-${STREAK_ACHIEVEMENTS[i].days}`);
  }
  for (const { count } of WORKOUT_ACHIEVEMENTS) {
    if (workouts >= count) keys.push(`workout-${count}`);
  }
  return keys;
}

export type AchievementKind = "streak" | "workout";

export function parseAchievementKey(key: AchievementKey): { kind: AchievementKind; threshold: number } | null {
  const [kind, raw] = key.split("-");
  const threshold = Number(raw);
  if ((kind !== "streak" && kind !== "workout") || !Number.isFinite(threshold)) return null;
  return { kind, threshold };
}

export function freshAchievementKeys(earned: AchievementKey[], seen: AchievementKey[]): AchievementKey[] {
  const seenSet = new Set(seen);
  const maxSeen: Record<AchievementKind, number> = { streak: 0, workout: 0 };
  for (const key of seen) {
    const parsed = parseAchievementKey(key);
    if (parsed && parsed.threshold > maxSeen[parsed.kind]) maxSeen[parsed.kind] = parsed.threshold;
  }
  return earned.filter((key) => {
    if (seenSet.has(key)) return false;
    const parsed = parseAchievementKey(key);
    return parsed ? parsed.threshold > maxSeen[parsed.kind] : false;
  });
}

/** Human-readable label for toasts: “7-day streak”, “10 workouts”. */
export function achievementLabel(key: AchievementKey, lang: UILang = "en"): string {
  const parsed = parseAchievementKey(key);
  if (lang === "ru") {
    if (!parsed) return "Новая ачивка";
    return parsed.kind === "streak"
      ? `Стрик ${parsed.threshold} ${daysWord(parsed.threshold, "ru")}`
      : `${parsed.threshold} ${workoutsWordRu(parsed.threshold, "ru")}`;
  }
  if (!parsed) return "New achievement";
  return parsed.kind === "streak"
    ? `${parsed.threshold}-day streak`
    : `${parsed.threshold} ${workoutsWordRu(parsed.threshold)}`;
}
