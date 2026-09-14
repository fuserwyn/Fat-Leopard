/** Прогресс недельной цели стаи (сброс каждое воскресенье). */
export const PACK_WEEKLY_GOAL_DEFAULT = 100;

export function packWeeklyProgressBarPct(completed: number, goal: number): number {
  if (!Number.isFinite(completed) || !Number.isFinite(goal) || goal <= 0) return 0;
  return Math.min(100, (Math.max(0, completed) / goal) * 100);
}

export function packWeeklyProgressLabel(completed: number, goal: number): string {
  const safeGoal = goal > 0 ? goal : PACK_WEEKLY_GOAL_DEFAULT;
  const safeCompleted = Math.max(0, Math.floor(completed));
  return `${safeCompleted}/${safeGoal}`;
}
