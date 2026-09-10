/** Совпадает с типами в NewWorkoutScreen / текстом отчёта в App.tsx (`kind`). */
export type WorkoutCategoryId =
  | "run"
  | "walk"
  | "bike"
  | "swim"
  | "yoga"
  | "rowing"
  | "workout"
  | "crossfit"
  | "stretch"
  | "dance"
  | "hiit"
  | "cardio"
  | "kettlebell"
  | "strength"
  | "jump_rope"
  | "pole"
  | "rollerblade"
  | "basketball"
  | "football"
  | "volleyball"
  | "tennis"
  | "padel"
  | "gymnastics"
  | "other";

export type WorkoutCategoryOption = { id: WorkoutCategoryId; label: string; emoji: string };

/** Порядок — как в форме новой тренировки (можно импортировать как список типов в форме). */
export const WORKOUT_CATEGORY_OPTIONS: WorkoutCategoryOption[] = [
  { id: "run", label: "Бег", emoji: "🏃" },
  { id: "walk", label: "Ходьба", emoji: "🚶" },
  { id: "bike", label: "Велосипед", emoji: "🚴" },
  { id: "swim", label: "Плавание", emoji: "🏊" },
  { id: "yoga", label: "Йога", emoji: "🧘" },
  { id: "rowing", label: "Гребля", emoji: "🚣" },
  { id: "workout", label: "Воркаут", emoji: "🔥" },
  { id: "crossfit", label: "Кроссфит", emoji: "🎯" },
  { id: "stretch", label: "Растяжка", emoji: "🧎" },
  { id: "dance", label: "Танцы", emoji: "💃" },
  { id: "hiit", label: "HIIT", emoji: "⚡" },
  { id: "cardio", label: "Кардио", emoji: "💓" },
  { id: "kettlebell", label: "Гиря", emoji: "🏋️" },
  { id: "strength", label: "Силовая", emoji: "🏋️" },
  { id: "jump_rope", label: "Скакалка", emoji: "🪢" },
  { id: "pole", label: "Пилон", emoji: "🤸" },
  { id: "rollerblade", label: "Ролики", emoji: "🛼" },
  { id: "basketball", label: "Баскетбол", emoji: "🏀" },
  { id: "football", label: "Футбол", emoji: "⚽" },
  { id: "volleyball", label: "Волейбол", emoji: "🏐" },
  { id: "tennis", label: "Теннис", emoji: "🎾" },
  { id: "padel", label: "Падел", emoji: "🏏" },
  { id: "gymnastics", label: "Гимнастика", emoji: "🙆" },
  { id: "other", label: "Другое", emoji: "✨" },
];

/** Для фильтра ленты: по русскому алфавиту названий (HIIT и латиница — по правилам `ru`). */
export const WORKOUT_CATEGORY_OPTIONS_ALPHABETICAL: WorkoutCategoryOption[] = [...WORKOUT_CATEGORY_OPTIONS].sort(
  (a, b) => a.label.localeCompare(b.label, "ru", { sensitivity: "base" }),
);

/**
 * Для вертикального списка «Все типы» в ленте: по алфавиту, но «Другое» всегда
 * в самом низу (это «прочее», а не буква «Д» в общем ряду).
 */
export const WORKOUT_CATEGORY_OPTIONS_ALPHABETICAL_OTHER_LAST: WorkoutCategoryOption[] = [
  ...WORKOUT_CATEGORY_OPTIONS_ALPHABETICAL.filter((o) => o.id !== "other"),
  ...WORKOUT_CATEGORY_OPTIONS_ALPHABETICAL.filter((o) => o.id === "other"),
];

const FEED_CAT_ORDER = new Map(WORKOUT_CATEGORY_OPTIONS_ALPHABETICAL.map((o, i) => [o.id, i]));

/** Порядок id как в алфавитном списке фильтра (для чипов «выбрано»). */
export function sortWorkoutCategoryIds(ids: readonly WorkoutCategoryId[]): WorkoutCategoryId[] {
  return [...ids].sort((a, b) => (FEED_CAT_ORDER.get(a) ?? 0) - (FEED_CAT_ORDER.get(b) ?? 0));
}

/** Алиас для экрана новой тренировки. */
export const WORKOUT_TYPES = WORKOUT_CATEGORY_OPTIONS;

const LABEL_TO_ID = Object.fromEntries(
  WORKOUT_CATEGORY_OPTIONS.map((o) => [o.label.trim().toLowerCase(), o.id]),
) as Record<string, WorkoutCategoryId>;

const ID_TO_EMOJI = Object.fromEntries(WORKOUT_CATEGORY_OPTIONS.map((o) => [o.id, o.emoji])) as Record<
  WorkoutCategoryId,
  string
>;

/** Эмодзи в заголовке карточки ленты для отчёта мини-аппа; если формат не распознан — 💪. */
export function trainingDoneCategoryEmoji(text: string): string {
  const cat = parseTrainingDoneCategory(text);
  if (cat === null) return "💪";
  return ID_TO_EMOJI[cat] ?? "💪";
}

/** Несколько видов в одном отчёте: «бег + плавание». Разделители — «+» и «/». */
const KIND_SPLIT_RE = /\s*[+/]\s*/;

/**
 * Заголовок карточки вместо общего «Тренировка»: тип из строки отчёта.
 * Для своего вида из поля «Другое» показываем то, что ввёл пользователь (а не «Другое»).
 */
export function trainingDoneCategoryDisplayLabel(text: string): string {
  const raw = parseTrainingDoneRawKind(text);
  if (raw === null) return "Другое";
  const id = LABEL_TO_ID[raw.toLowerCase()];
  if (id) return WORKOUT_CATEGORY_OPTIONS.find((o) => o.id === id)?.label ?? "Другое";
  return raw;
}

/** Сырой вид активности из первой строки отчёта (с исходным регистром), либо null. */
function parseTrainingDoneRawKind(text: string): string | null {
  const line = (text.trim().split("\n")[0] ?? "").trim();
  const m = line.match(/^(?:#training_done\s*[—–-]\s*)?([^,]+),\s*\d+\s*мин/i);
  if (!m) return null;
  return m[1].trim();
}

/** Один вид из «сырой» подписи (нижний регистр → id); неизвестный → "other". */
function rawKindToCategory(raw: string): WorkoutCategoryId {
  return LABEL_TO_ID[raw.trim().toLowerCase()] ?? "other";
}

/**
 * Первая строка отчёта: «бег, 15 мин, инт. 3/5» (старые — с префиксом #training_done).
 * При мультивыборе возвращает первый вид; для полного списка — {@link parseTrainingDoneCategories}.
 */
export function parseTrainingDoneCategory(text: string): WorkoutCategoryId | null {
  const cats = parseTrainingDoneCategories(text);
  return cats.length > 0 ? cats[0] : null;
}

/** Все виды из отчёта (мультивыбор «бег + плавание» → ["run","swim"]); пустой массив, если формат не распознан. */
export function parseTrainingDoneCategories(text: string): WorkoutCategoryId[] {
  const raw = parseTrainingDoneRawKind(text);
  if (raw === null) return [];
  const seen = new Set<WorkoutCategoryId>();
  const ids: WorkoutCategoryId[] = [];
  for (const part of raw.split(KIND_SPLIT_RE)) {
    if (!part.trim()) continue;
    const id = rawKindToCategory(part);
    if (!seen.has(id)) {
      seen.add(id);
      ids.push(id);
    }
  }
  return ids;
}

/**
 * Убирает ведущее «бег, » / «плавание, » из тела поста — тип уже в заголовке карточки.
 * Формат: «вид, N мин, инт. …» (+ опциональный комментарий на следующих строках).
 */
export function stripLeadingCategoryFromTrainingReport(text: string): string {
  const trimmed = text.trim();
  if (!trimmed) return trimmed;

  const withoutTag = trimmed.replace(/^#training_done\s*[—–-]\s*/i, "").trim();
  const nl = withoutTag.indexOf("\n");
  const firstLine = nl >= 0 ? withoutTag.slice(0, nl) : withoutTag;
  const rest = nl >= 0 ? withoutTag.slice(nl + 1).trim() : "";

  const m = firstLine.match(/^([^,]+),\s*(.+)$/);
  if (!m) return trimmed;
  const afterKind = m[2].trim();
  if (!/\d+\s*мин/i.test(afterKind)) return trimmed;

  const body = rest ? `${afterKind}\n${rest}` : afterKind;
  return body.trim();
}

/** Совпадение с выбранным фильтром категории (для `training_done`). Пост с несколькими видами матчится по любому из них. */
export function trainingDoneMatchesCategory(text: string, filterId: WorkoutCategoryId): boolean {
  const cats = parseTrainingDoneCategories(text);
  if (cats.length === 0) return filterId === "other";
  return cats.includes(filterId);
}

/** Мультивыбор фильтра: отчёт попадает в ленту, если хотя бы один его вид есть в `selected` (не пустом). */
export function trainingDoneMatchesAnyCategory(
  text: string,
  selected: ReadonlySet<WorkoutCategoryId>,
): boolean {
  if (selected.size === 0) return true;
  const cats = parseTrainingDoneCategories(text);
  if (cats.length === 0) return selected.has("other");
  return cats.some((c) => selected.has(c));
}
