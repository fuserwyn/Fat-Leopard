import { daysSinceLastTrainingFromDate, parseLocalDateYMD } from "./streakLabel";

export type WorkoutDayPoint = {
  date: string;
  count: number;
};

export type ProfileChartDay = {
  date: string;
  value: number;
};

/** YYYY-MM-DD из локальной даты. */
export function formatLocalDateYMD(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function addDaysYMD(ymd: string, delta: number): string {
  const d = parseLocalDateYMD(ymd);
  d.setDate(d.getDate() + delta);
  return formatLocalDateYMD(d);
}

/** Порт ComputeStreakDays с бэкенда — новый стрик после отчёта в день todayYmd. */
export function computeStreakDaysAfterReport(
  lastTrainingDate: string | null,
  prevStreak: number,
  todayYmd: string,
): number {
  const todayStr = todayYmd.trim();
  const last = lastTrainingDate?.trim() ?? "";

  if (last === todayStr) return prevStreak;
  if (last === "") {
    if (prevStreak > 0) return prevStreak + 1;
    return 1;
  }
  if (last === addDaysYMD(todayStr, -1)) return prevStreak + 1;
  return 1;
}

/** EffectiveStreakDays на конец календарного дня без нового отчёта. */
export function effectiveStreakAtDay(storedStreak: number, daysSinceLastTraining: number): number {
  if (storedStreak <= 0) return 0;
  if (daysSinceLastTraining < 0) return storedStreak;
  if (daysSinceLastTraining >= 2) return 0;
  return storedStreak;
}

/** Стрик по дням — восстановление по тем же правилам, что на бэкенде. */
export function buildStreakByDay(workoutsByDay: WorkoutDayPoint[]): ProfileChartDay[] {
  let lastTrainingDate: string | null = null;
  let storedStreak = 0;

  return workoutsByDay.map(({ date, count }) => {
    if (count > 0) {
      storedStreak = computeStreakDaysAfterReport(lastTrainingDate, storedStreak, date);
      lastTrainingDate = date;
      return { date, value: storedStreak };
    }
    const daysSince = lastTrainingDate
      ? daysSinceLastTrainingFromDate(lastTrainingDate, parseLocalDateYMD(date))
      : -1;
    return { date, value: effectiveStreakAtDay(storedStreak, daysSince) };
  });
}

export function workoutsToChartDays(workoutsByDay: WorkoutDayPoint[]): ProfileChartDay[] {
  return workoutsByDay.map(({ date, count }) => ({ date, value: count }));
}

/** Короткая подпись оси: «2 сен». */
export function formatChartDayLabel(ymd: string): string {
  const d = parseLocalDateYMD(ymd);
  return d.toLocaleDateString("en-US", { day: "numeric", month: "short" });
}

export function parseWorkoutsByDay(raw: unknown): WorkoutDayPoint[] {
  if (!Array.isArray(raw)) return [];
  const out: WorkoutDayPoint[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object") continue;
    const date = (item as { date?: unknown }).date;
    const count = (item as { count?: unknown }).count;
    if (typeof date !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(date)) continue;
    if (typeof count !== "number" || !Number.isFinite(count) || count < 0) continue;
    out.push({ date, count: Math.trunc(count) });
  }
  return out;
}
