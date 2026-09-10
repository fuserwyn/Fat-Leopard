import {
  WORKOUT_CATEGORY_OPTIONS,
  type WorkoutCategoryId,
  type WorkoutCategoryOption,
} from "./workoutCategories";

const DEFAULT_ORDER = WORKOUT_CATEGORY_OPTIONS.map((o) => o.id);
const DEFAULT_INDEX = new Map(DEFAULT_ORDER.map((id, i) => [id, i]));

/** Переставляет типы: сначала suggestedIds с сервера, остальные — в исходном порядке. */
export function orderWorkoutTypes(
  options: readonly WorkoutCategoryOption[],
  suggestedIds?: readonly WorkoutCategoryId[] | null,
): WorkoutCategoryOption[] {
  if (!suggestedIds?.length) return [...options];
  const rank = new Map<WorkoutCategoryId, number>();
  for (const id of suggestedIds) {
    if (DEFAULT_INDEX.has(id) && !rank.has(id)) {
      rank.set(id, rank.size);
    }
  }
  if (rank.size === 0) return [...options];
  return [...options].sort((a, b) => {
    const ra = rank.has(a.id) ? rank.get(a.id)! : 1000 + (DEFAULT_INDEX.get(a.id) ?? 999);
    const rb = rank.has(b.id) ? rank.get(b.id)! : 1000 + (DEFAULT_INDEX.get(b.id) ?? 999);
    return ra - rb;
  });
}

/** Нормализует ответ profile/load. */
export function parseSuggestedWorkoutTypes(raw: unknown): WorkoutCategoryId[] | null {
  if (!Array.isArray(raw) || raw.length === 0) return null;
  const out: WorkoutCategoryId[] = [];
  for (const item of raw) {
    if (typeof item !== "string") continue;
    const id = item as WorkoutCategoryId;
    if (DEFAULT_INDEX.has(id) && !out.includes(id)) out.push(id);
  }
  return out.length > 0 ? out : null;
}
