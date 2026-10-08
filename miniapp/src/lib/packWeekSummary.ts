/** Итоги недели стаи: модалка, которую Лео показывает участникам общего зачёта в понедельник. */

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

/** На сколько тренировок растёт цель стаи после закрытой недели (как PackWeeklyGoalStep в ms_leo). */
export const PACK_WEEKLY_GOAL_STEP = 5;

export type PackWeekSummary = {
  weekStart: string;
  weekEnd: string;
  workouts: number;
  goal: number;
  goalReached: boolean;
  nextGoal: number;
  participants: number;
  myWorkouts: number;
};

const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) && v > 0 ? Math.floor(v) : 0);

/** Разбор ответа сервера; null — показывать нечего. */
export function parsePackWeekSummary(raw: unknown): PackWeekSummary | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const weekStart = typeof r.week_start === "string" ? r.week_start.trim() : "";
  const goal = num(r.goal);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(weekStart) || goal === 0) return null;
  return {
    weekStart,
    weekEnd: typeof r.week_end === "string" ? r.week_end.trim() : "",
    workouts: num(r.workouts),
    goal,
    goalReached: r.goal_reached === true,
    nextGoal: num(r.next_goal) || goal,
    participants: num(r.participants),
    myWorkouts: num(r.my_workouts),
  };
}

const MONTHS = ["января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"];

function dayMonth(date: string): { day: number; month: string } | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!m) return null;
  const month = MONTHS[Number(m[2]) - 1];
  if (!month) return null;
  return { day: Number(m[3]), month };
}

/** «5–11 октября» или «28 сентября – 4 октября». */
export function packWeekRangeLabel(weekStart: string, weekEnd: string): string {
  const a = dayMonth(weekStart);
  const b = dayMonth(weekEnd);
  if (!a) return "";
  if (!b) return `с ${a.day} ${a.month}`;
  if (a.month === b.month) return `${a.day}–${b.day} ${b.month}`;
  return `${a.day} ${a.month} – ${b.day} ${b.month}`;
}

/** Склонение «тренировка» по числу. */
export function workoutsWord(n: number): string {
  const n10 = n % 10;
  const n100 = n % 100;
  if (n10 === 1 && n100 !== 11) return "тренировка";
  if (n10 >= 2 && n10 <= 4 && (n100 < 12 || n100 > 14)) return "тренировки";
  return "тренировок";
}

/** Главная фраза модалки. */
export function packWeekSummaryHeadline(s: PackWeekSummary): string {
  return s.goalReached ? "Цель недели закрыта!" : "Цель недели не закрыта";
}

/** Что дальше: новая цель или та же. */
export function packWeekSummaryNextLine(s: PackWeekSummary): string {
  if (s.goalReached && s.nextGoal > s.goal) {
    return `Планка растёт: цель новой недели — ${s.nextGoal} ${workoutsWord(s.nextGoal)} (+${s.nextGoal - s.goal}).`;
  }
  if (s.goalReached) return `Цель новой недели — ${s.nextGoal} ${workoutsWord(s.nextGoal)}.`;
  const left = Math.max(0, s.goal - s.workouts);
  return `Не хватило ${left} ${workoutsWord(left)}. Цель новой недели та же — ${s.nextGoal}. Закроем её вместе!`;
}

export async function fetchPackWeekSummary(initData: string): Promise<PackWeekSummary | null> {
  if (!apiBase || !initData.trim()) return null;
  try {
    const res = await fetch(`${apiBase}/api/miniapp/pack/week-summary`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData }),
    });
    const j = (await res.json().catch(() => ({}))) as { ok?: boolean; summary?: unknown };
    if (!res.ok || !j.ok) return null;
    return parsePackWeekSummary(j.summary);
  } catch {
    return null;
  }
}

export async function markPackWeekSummarySeen(initData: string, weekStart: string): Promise<boolean> {
  if (!apiBase || !initData.trim() || !weekStart) return false;
  try {
    const res = await fetch(`${apiBase}/api/miniapp/pack/week-summary/seen`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData, week_start: weekStart }),
    });
    const j = (await res.json().catch(() => ({}))) as { ok?: boolean };
    return res.ok && Boolean(j.ok);
  } catch {
    return false;
  }
}
