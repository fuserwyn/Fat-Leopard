import type { UILang } from "./i18n";

function workoutsWordRu(n: number): string {
  const mod100 = n % 100;
  const mod10 = n % 10;
  if (mod100 >= 11 && mod100 <= 14) return "тренировок";
  if (mod10 === 1) return "тренировку";
  if (mod10 >= 2 && mod10 <= 4) return "тренировки";
  return "тренировок";
}

/** Прогресс недельной цели стаи (сброс каждый понедельник 00:00 МСК). */
export const PACK_WEEKLY_GOAL_DEFAULT = 75;

/** Сегментов в прогресс-баре «Неделя стаи». */
export const PACK_WEEKLY_PROGRESS_STEPS = 5;

/** Подсказка к недельной цели стаи для конкретной цели (берётся с сервера). */
export function packWeeklyGoalHint(goal: number = PACK_WEEKLY_GOAL_DEFAULT, lang: UILang = "en"): string {
  const safeGoal = Number.isFinite(goal) && goal > 0 ? Math.floor(goal) : PACK_WEEKLY_GOAL_DEFAULT;
  if (lang === "ru") {
    return `Если стая запишет ${safeGoal} ${workoutsWordRu(safeGoal)} за неделю, все получат 50 бонусных кубков. Когда неделя закончится, цель на следующую вырастет на 5 тренировок.`;
  }
  return `If the pack logs ${safeGoal} workouts this week, everyone gets 50 bonus cups. When the week closes, next week’s goal goes up by 5 workouts.`;
}

/** Подсказка к недельной цели стаи (иконка «?» у прогресс-бара). */
export const PACK_WEEKLY_GOAL_HINT = packWeeklyGoalHint(PACK_WEEKLY_GOAL_DEFAULT, "en");

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
