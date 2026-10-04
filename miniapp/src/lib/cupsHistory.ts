import { trainingDoneCategoryDisplayLabel } from "./workoutCategories";
import { cupsWordRu } from "./streakLabel";

/** Сколько последних тренировок показываем в истории кубков. */
export const CUPS_HISTORY_LIMIT = 42;

/** Подпись списка в шторке профиля. */
export const CUPS_HISTORY_CAPTION = "42 последние тренировки";

/** Вид строки для кубков за недельное достижение стаи. */
export const PACK_WEEKLY_CUPS_LABEL = "Недельное достижение стаи";

export type CupsHistoryWorkoutInput = {
  date: string;
  messageText: string;
  cups: number;
  createdAt: string;
};

export type CupsHistoryWeeklyInput = {
  date: string;
  cups: number;
  createdAt: string;
};

export type CupsHistoryRow = {
  kind: "workout" | "pack_weekly";
  dateLabel: string;
  workoutType: string;
  intensity: string;
  duration: string;
  cupsLabel: string;
  /** Начислено кубков (целое, >= 0) — для сортировки. */
  cups: number;
  /** Календарный день YYYY-MM-DD — для сортировки по дате. */
  dateKey: string;
  line: string;
  sortAt: number;
};

export type CupsHistorySortKey = "date" | "type" | "cups";
export type CupsHistorySortDir = "asc" | "desc";

export const CUPS_HISTORY_SORT_OPTIONS: ReadonlyArray<{ key: CupsHistorySortKey; label: string }> = [
  { key: "date", label: "Дата" },
  { key: "type", label: "Вид спорта" },
  { key: "cups", label: "Кубки" },
];

/** Направление по умолчанию при выборе ключа: дата и кубки — по убыванию, вид спорта — по алфавиту. */
export function defaultCupsHistorySortDir(key: CupsHistorySortKey): CupsHistorySortDir {
  return key === "type" ? "asc" : "desc";
}

function timeOf(iso: string): number {
  const t = Date.parse(iso);
  return Number.isNaN(t) ? Number.NEGATIVE_INFINITY : t;
}

/** YYYY-MM-DD → ДД.ММ.ГГГГ. Не сдвигает календарный день через часовой пояс. */
export function formatCupsHistoryDate(ymd: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(ymd.trim());
  if (!m) return ymd.trim();
  return `${m[3]}.${m[2]}.${m[1]}`;
}

function parseDurationMin(text: string): number | null {
  const line = (text.trim().split("\n")[0] ?? "").trim();
  const m = line.match(/(\d+)\s*мин/i);
  if (!m) return null;
  const n = Number(m[1]);
  return Number.isFinite(n) && n > 0 ? n : null;
}

function parseIntensity(text: string): number | null {
  const line = (text.trim().split("\n")[0] ?? "").trim();
  const m = line.match(/(?:интенсивность|инт\.?)\s*(\d+)/i);
  if (!m) return null;
  const n = Number(m[1]);
  return Number.isFinite(n) && n > 0 ? n : null;
}

function cupsLabel(n: number): string {
  const cups = Math.max(0, Math.trunc(n));
  return `+${cups} ${cupsWordRu(cups)}`;
}

function historyLine(dateLabel: string, workoutType: string, intensity: string, duration: string, awarded: string): string {
  return [dateLabel, workoutType, intensity, duration, awarded].filter((part) => part !== "").join(" ");
}

function dateKeyOf(ymd: string): string {
  const m = /^(\d{4}-\d{2}-\d{2})/.exec(ymd.trim());
  return m ? m[1] : "";
}

function workoutRow(input: CupsHistoryWorkoutInput): CupsHistoryRow {
  const dateLabel = formatCupsHistoryDate(input.date);
  const workoutType = trainingDoneCategoryDisplayLabel(input.messageText);
  const intensityN = parseIntensity(input.messageText);
  const durationN = parseDurationMin(input.messageText);
  const intensity = intensityN == null ? "" : `${intensityN}/5`;
  const duration = durationN == null ? "" : `${durationN} мин`;
  const awarded = cupsLabel(input.cups);
  return {
    kind: "workout",
    dateLabel,
    workoutType,
    intensity,
    duration,
    cupsLabel: awarded,
    cups: Math.max(0, Math.trunc(input.cups)),
    dateKey: dateKeyOf(input.date),
    line: historyLine(dateLabel, workoutType, intensity, duration, awarded),
    sortAt: timeOf(input.createdAt),
  };
}

function weeklyRow(input: CupsHistoryWeeklyInput): CupsHistoryRow {
  const dateLabel = formatCupsHistoryDate(input.date);
  const awarded = cupsLabel(input.cups);
  return {
    kind: "pack_weekly",
    dateLabel,
    workoutType: PACK_WEEKLY_CUPS_LABEL,
    intensity: "",
    duration: "",
    cupsLabel: awarded,
    cups: Math.max(0, Math.trunc(input.cups)),
    dateKey: dateKeyOf(input.date),
    line: historyLine(dateLabel, PACK_WEEKLY_CUPS_LABEL, "", "", awarded),
    sortAt: timeOf(input.createdAt),
  };
}

/**
 * Последние `limit` тренировок и дополнительные кубки за неделю стаи.
 * Пока тренировок не больше лимита, бонусы стаи показываются все.
 * Если тренировок больше, бонус остаётся, только если он не старше самой ранней
 * из этих тренировок. Новые сверху.
 */
export function buildCupsHistoryRows(
  workouts: readonly CupsHistoryWorkoutInput[],
  weekly: readonly CupsHistoryWeeklyInput[],
  limit = CUPS_HISTORY_LIMIT,
): CupsHistoryRow[] {
  const safeLimit = limit > 0 ? limit : CUPS_HISTORY_LIMIT;
  const ordered = [...workouts].sort((a, b) => timeOf(b.createdAt) - timeOf(a.createdAt));
  const selected = ordered.slice(0, safeLimit);
  const clipped = ordered.length > safeLimit;
  const oldest = selected.reduce((min, row) => Math.min(min, timeOf(row.createdAt)), Number.POSITIVE_INFINITY);
  const weeklyRows = weekly
    .filter((row) => row.cups > 0)
    .filter((row) => !clipped || timeOf(row.createdAt) >= oldest)
    .map(weeklyRow);
  const rows = [...selected.map(workoutRow), ...weeklyRows];
  rows.sort((a, b) => {
    if (b.sortAt !== a.sortAt) return b.sortAt - a.sortAt;
    if (a.kind === b.kind) return 0;
    return a.kind === "workout" ? -1 : 1;
  });
  return rows;
}

function compareChrono(a: CupsHistoryRow, b: CupsHistoryRow): number {
  if (a.dateKey !== b.dateKey) return a.dateKey < b.dateKey ? -1 : 1;
  if (a.sortAt !== b.sortAt) return a.sortAt < b.sortAt ? -1 : 1;
  if (a.kind === b.kind) return 0;
  return a.kind === "workout" ? 1 : -1;
}

/**
 * Сортировка уже собранных строк истории. Не меняет исходный массив.
 * При равенстве по ключу строки идут от новых к старым.
 */
export function sortCupsHistoryRows(
  rows: readonly CupsHistoryRow[],
  key: CupsHistorySortKey,
  dir: CupsHistorySortDir = defaultCupsHistorySortDir(key),
): CupsHistoryRow[] {
  const sign = dir === "asc" ? 1 : -1;
  return [...rows].sort((a, b) => {
    let primary = 0;
    if (key === "type") {
      primary = a.workoutType.localeCompare(b.workoutType, "ru", { sensitivity: "base" });
    } else if (key === "cups") {
      primary = a.cups - b.cups;
    } else {
      primary = compareChrono(a, b);
    }
    if (primary !== 0) return sign * primary;
    return -compareChrono(a, b);
  });
}
