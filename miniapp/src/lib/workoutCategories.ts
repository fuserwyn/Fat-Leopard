import type { UILang } from "./i18n";

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
  | "morning_exercise"
  | "climbing"
  | "other";

export type WorkoutCategoryOption = { id: WorkoutCategoryId; label: string; emoji: string };

/** Порядок — как в форме новой тренировки (можно импортировать как список типов в форме). */
export const WORKOUT_CATEGORY_OPTIONS: WorkoutCategoryOption[] = [
  { id: "run", label: "Run", emoji: "🏃" },
  { id: "walk", label: "Walk", emoji: "🚶" },
  { id: "bike", label: "Cycling", emoji: "🚴" },
  { id: "swim", label: "Swim", emoji: "🏊" },
  { id: "yoga", label: "Yoga", emoji: "🧘" },
  { id: "rowing", label: "Rowing", emoji: "🚣" },
  { id: "workout", label: "Workout", emoji: "🔥" },
  { id: "crossfit", label: "CrossFit", emoji: "🎯" },
  { id: "stretch", label: "Stretch", emoji: "🧎" },
  { id: "dance", label: "Dance", emoji: "💃" },
  { id: "hiit", label: "HIIT", emoji: "⚡" },
  { id: "cardio", label: "Cardio", emoji: "💓" },
  { id: "kettlebell", label: "Kettlebell", emoji: "🏋️" },
  { id: "strength", label: "Strength", emoji: "🏋️" },
  { id: "jump_rope", label: "Jump rope", emoji: "🪢" },
  { id: "pole", label: "Pole", emoji: "🤸" },
  { id: "rollerblade", label: "Rollerblading", emoji: "🛼" },
  { id: "basketball", label: "Basketball", emoji: "🏀" },
  { id: "football", label: "Football", emoji: "⚽" },
  { id: "volleyball", label: "Volleyball", emoji: "🏐" },
  { id: "tennis", label: "Tennis", emoji: "🎾" },
  { id: "padel", label: "Padel", emoji: "🏏" },
  { id: "gymnastics", label: "Gymnastics", emoji: "🙆" },
  { id: "morning_exercise", label: "Morning mobility", emoji: "☀️" },
  { id: "climbing", label: "Climbing", emoji: "🧗" },
  { id: "other", label: "Other", emoji: "✨" },
];

/** Русские подписи видов (совпадают с legacy-разбором старых отчётов). */
const WORKOUT_CATEGORY_LABELS_RU: Record<WorkoutCategoryId, string> = {
  run: "Бег",
  walk: "Ходьба",
  bike: "Велосипед",
  swim: "Плавание",
  yoga: "Йога",
  rowing: "Гребля",
  workout: "Воркаут",
  crossfit: "Кроссфит",
  stretch: "Растяжка",
  dance: "Танцы",
  hiit: "HIIT",
  cardio: "Кардио",
  kettlebell: "Гиря",
  strength: "Силовая",
  jump_rope: "Скакалка",
  pole: "Пилон",
  rollerblade: "Ролики",
  basketball: "Баскетбол",
  football: "Футбол",
  volleyball: "Волейбол",
  tennis: "Теннис",
  padel: "Падел",
  gymnastics: "Гимнастика",
  morning_exercise: "Зарядка",
  climbing: "Скалолазание",
  other: "Другое",
};

const WORKOUT_CATEGORY_LABELS_EN = Object.fromEntries(
  WORKOUT_CATEGORY_OPTIONS.map((o) => [o.id, o.label]),
) as Record<WorkoutCategoryId, string>;

function sortLocale(lang: UILang): string {
  return lang === "ru" ? "ru" : "en";
}

/** Подпись вида на языке интерфейса. */
export function workoutCategoryLabel(id: WorkoutCategoryId, lang: UILang = "en"): string {
  const map = lang === "ru" ? WORKOUT_CATEGORY_LABELS_RU : WORKOUT_CATEGORY_LABELS_EN;
  return map[id] ?? id;
}

/** Все виды в порядке формы новой тренировки с локализованными подписями. */
export function workoutCategoryOptions(lang: UILang = "en"): WorkoutCategoryOption[] {
  return WORKOUT_CATEGORY_OPTIONS.map((o) => ({ ...o, label: workoutCategoryLabel(o.id, lang) }));
}

/** Для фильтра ленты: по алфавиту подписей выбранного языка. */
export function workoutCategoriesAlphabetical(lang: UILang = "en"): WorkoutCategoryOption[] {
  const locale = sortLocale(lang);
  return workoutCategoryOptions(lang).sort((a, b) =>
    a.label.localeCompare(b.label, locale, { sensitivity: "base" }),
  );
}

/**
 * Для вертикального списка «Все типы» в ленте: по алфавиту, но «Другое» всегда
 * в самом низу (это «прочее», а не буква «Д» в общем ряду).
 */
export function workoutCategoriesAlphabeticalOtherLast(lang: UILang = "en"): WorkoutCategoryOption[] {
  const sorted = workoutCategoriesAlphabetical(lang);
  return [...sorted.filter((o) => o.id !== "other"), ...sorted.filter((o) => o.id === "other")];
}

/** Для фильтра ленты (английский алфавит; для локализации — {@link workoutCategoriesAlphabetical}). */
export const WORKOUT_CATEGORY_OPTIONS_ALPHABETICAL: WorkoutCategoryOption[] = workoutCategoriesAlphabetical("en");

/** «Все типы» в ленте, «Другое» внизу (английский; см. {@link workoutCategoriesAlphabeticalOtherLast}). */
export const WORKOUT_CATEGORY_OPTIONS_ALPHABETICAL_OTHER_LAST: WorkoutCategoryOption[] =
  workoutCategoriesAlphabeticalOtherLast("en");

const FEED_CAT_ORDER = new Map(WORKOUT_CATEGORY_OPTIONS_ALPHABETICAL.map((o, i) => [o.id, i]));

function workoutCategoryCount(
  counts: Readonly<Partial<Record<WorkoutCategoryId, number>>> | undefined,
  id: WorkoutCategoryId,
): number {
  return counts?.[id] ?? 0;
}

function compareWorkoutCategoriesByCountDesc(
  a: WorkoutCategoryId,
  b: WorkoutCategoryId,
  counts: Readonly<Partial<Record<WorkoutCategoryId, number>>>,
  lang: UILang = "en",
): number {
  const ca = workoutCategoryCount(counts, a);
  const cb = workoutCategoryCount(counts, b);
  if (cb !== ca) return cb - ca;
  if (a === "other") return 1;
  if (b === "other") return -1;
  const la = workoutCategoryLabel(a, lang);
  const lb = workoutCategoryLabel(b, lang);
  return la.localeCompare(lb, sortLocale(lang), { sensitivity: "base" });
}

/** Сортировка видов по убыванию частоты в стае; «Другое» — в конце при равных счётчиках. */
export function sortWorkoutCategoriesByCountDesc(
  options: readonly WorkoutCategoryOption[],
  counts: Readonly<Partial<Record<WorkoutCategoryId, number>>>,
  lang: UILang = "en",
): WorkoutCategoryOption[] {
  return [...options].sort((a, b) => compareWorkoutCategoriesByCountDesc(a.id, b.id, counts, lang));
}

/** Порядок id в фильтре: по частоте (если есть counts), иначе по алфавиту. */
export function sortWorkoutCategoryIds(
  ids: readonly WorkoutCategoryId[],
  counts?: Readonly<Partial<Record<WorkoutCategoryId, number>>>,
  lang: UILang = "en",
): WorkoutCategoryId[] {
  if (counts && Object.keys(counts).length > 0) {
    return [...ids].sort((a, b) => compareWorkoutCategoriesByCountDesc(a, b, counts, lang));
  }
  const order = lang === "en" ? FEED_CAT_ORDER : new Map(workoutCategoriesAlphabetical(lang).map((o, i) => [o.id, i]));
  return [...ids].sort((a, b) => (order.get(a) ?? 0) - (order.get(b) ?? 0));
}

/** Алиас для экрана новой тренировки. */
export const WORKOUT_TYPES = WORKOUT_CATEGORY_OPTIONS;

/** EN + RU подписи (нижний регистр) → id. */
const LABEL_TO_ID = Object.fromEntries(
  WORKOUT_CATEGORY_OPTIONS.flatMap((o) => [
    [o.label.trim().toLowerCase(), o.id],
    [WORKOUT_CATEGORY_LABELS_RU[o.id].trim().toLowerCase(), o.id],
  ]),
) as Record<string, WorkoutCategoryId>;

/** Подпись известна (EN/RU/legacy) — не пользовательский вид из «Другое». */
export function isKnownWorkoutCategoryLabel(label: string): boolean {
  const key = label.trim().toLowerCase();
  return key in LABEL_TO_ID || key in LEGACY_LABEL_TO_ID;
}

/** Legacy Russian labels in stored reports (pre-English UI). */
const LEGACY_LABEL_TO_ID: Record<string, WorkoutCategoryId> = {
  бег: "run",
  ходьба: "walk",
  велосипед: "bike",
  плавание: "swim",
  йога: "yoga",
  гребля: "rowing",
  воркаут: "workout",
  кроссфит: "crossfit",
  растяжка: "stretch",
  танцы: "dance",
  кардио: "cardio",
  гиря: "kettlebell",
  силовая: "strength",
  скакалка: "jump_rope",
  пилон: "pole",
  ролики: "rollerblade",
  баскетбол: "basketball",
  футбол: "football",
  волейбол: "volleyball",
  теннис: "tennis",
  падел: "padel",
  гимнастика: "gymnastics",
  зарядка: "morning_exercise",
  скалолазание: "climbing",
  другое: "other",
};

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
export function trainingDoneCategoryDisplayLabel(text: string, lang: UILang = "en"): string {
  const raw = parseTrainingDoneRawKind(text);
  if (raw === null) return workoutCategoryLabel("other", lang);
  const labelFor = (part: string): string => {
    const id = rawKindToCategory(part);
    if (id !== "other" || isKnownWorkoutCategoryLabel(part)) return workoutCategoryLabel(id, lang);
    return part.trim();
  };
  const parts = raw.split(KIND_SPLIT_RE).filter((p) => p.trim());
  if (parts.length > 1) return parts.map(labelFor).join(" + ");
  return labelFor(raw);
}

/** Сырой вид активности из первой строки отчёта (с исходным регистром), либо null. */
function parseTrainingDoneRawKind(text: string): string | null {
  const line = (text.trim().split("\n")[0] ?? "").trim();
  const m = line.match(/^(?:#training_done\s*[—–-]\s*)?([^,]+),\s*\d+\s*(?:мин|min)/i);
  if (!m) return null;
  return m[1].trim();
}

/** Один вид из «сырой» подписи (нижний регистр → id); неизвестный → "other". */
function rawKindToCategory(raw: string): WorkoutCategoryId {
  const key = raw.trim().toLowerCase();
  return LABEL_TO_ID[key] ?? LEGACY_LABEL_TO_ID[key] ?? "other";
}

/**
 * Первая строка отчёта: «бег, 15 мин, интенсивность 3/5» (старые — «инт.»; префикс #training_done опционален).
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

/** В ленте показываем полное слово вместо устаревшего «инт.». */
export function expandTrainingIntensityLabel(text: string): string {
  return text
    .replace(/инт\.\s*/gi, "intensity ")
    .replace(/интенсивность\s*/gi, "intensity ");
}

/**
 * Убирает ведущее «бег, » / «плавание, » из тела поста — тип уже в заголовке карточки.
 * Формат: «вид, N мин, интенсивность …» (+ опциональный комментарий на следующих строках).
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
  if (!/\d+\s*(?:мин|min)/i.test(afterKind)) return trimmed;

  const body = rest ? `${afterKind}\n${rest}` : afterKind;
  return expandTrainingIntensityLabel(body.trim());
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
