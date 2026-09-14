/** Прогресс недельной цели стаи (сброс каждый понедельник 00:00 МСК). */
export const PACK_WEEKLY_GOAL_DEFAULT = 50;

/** Сегментов в прогресс-баре «Неделя стаи». */
export const PACK_WEEKLY_PROGRESS_STEPS = 5;

/** Подсказка к недельной цели стаи (иконка «?» у прогресс-бара). */
export const PACK_WEEKLY_GOAL_HINT =
  "Если стая выполнит за неделю 50 тренировок, каждый получит 50 дополнительных кубков.";

export function packWeeklyProgressBarPct(completed: number, goal: number): number {
  if (!Number.isFinite(completed) || !Number.isFinite(goal) || goal <= 0) return 0;
  return Math.min(100, (Math.max(0, completed) / goal) * 100);
}

/** Заливка каждого из 5 шагов прогресса (0–100%). */
export function packWeeklyStepFillPcts(
  completed: number,
  goal: number,
  steps = PACK_WEEKLY_PROGRESS_STEPS,
): number[] {
  const safeGoal = goal > 0 ? goal : PACK_WEEKLY_GOAL_DEFAULT;
  const safeCompleted = Math.max(0, Number.isFinite(completed) ? completed : 0);
  const stepSize = safeGoal / steps;
  if (stepSize <= 0) return Array.from({ length: steps }, () => 0);

  return Array.from({ length: steps }, (_, i) => {
    const stepStart = i * stepSize;
    const stepEnd = (i + 1) * stepSize;
    if (safeCompleted >= stepEnd) return 100;
    if (safeCompleted <= stepStart) return 0;
    return Math.min(100, ((safeCompleted - stepStart) / stepSize) * 100);
  });
}

export function packWeeklyProgressLabel(completed: number, goal: number): string {
  const safeGoal = goal > 0 ? goal : PACK_WEEKLY_GOAL_DEFAULT;
  const safeCompleted = Math.max(0, Math.floor(completed));
  return `${safeCompleted}/${safeGoal}`;
}
