/**
 * Накопленные кубки на границах уровней (шкала 1, §2.5): L1 с 0, L2 с 420, … L6 Слон с 13 020.
 * Индекс i = минимум кубков для уровня (i+1).
 */
export const CUP_LEVEL_STARTS: readonly number[] = [0, 420, 1260, 2940, 6300, 13020];

/** Максимальный уровень (6 — Слон). */
export const MAX_CUP_LEVEL = CUP_LEVEL_STARTS.length;

/**
 * Имена уровней-животных. Источник истины — leopardmoney.LevelNames на бэке
 * (ms_leo/internal/game/leopardmoney/const.go). Индекс = номер уровня (1-based);
 * "" на индексе 0 — нулевой уровень не используется.
 */
export const MINIAPP_LEVEL_NAMES: readonly string[] = ["", "Сурикат", "Газель", "Зебра", "Гепард", "Лев", "Слон"];

/**
 * Эмодзи-животные уровней (индекс = номер уровня 1-based, как в правилах Стаи).
 * Отдельного эмодзи суриката нет — используется 🦫, как в таблице уровней в правилах.
 */
export const MINIAPP_LEVEL_EMOJI: readonly string[] = ["", "🦫", "🦌", "🦓", "🐆", "🦁", "🐘"];

/** Эмодзи уровня по номеру 1-based. */
export function miniappLevelEmoji(level: number): string {
  if (level < 1) return "";
  if (level >= MINIAPP_LEVEL_EMOJI.length) return MINIAPP_LEVEL_EMOJI[MINIAPP_LEVEL_EMOJI.length - 1] ?? "";
  return MINIAPP_LEVEL_EMOJI[level] ?? "";
}

/** Минимум накопленных кубков для уровня (1-based). */
export function miniappLevelMinCups(level: number): number {
  const idx = Math.min(Math.max(1, Math.floor(level)), MAX_CUP_LEVEL) - 1;
  return CUP_LEVEL_STARTS[idx] ?? 0;
}

/** Имя уровня по номеру 1-based. */
export function miniappLevelName(level: number): string {
  if (level < 1) return "";
  if (level >= MINIAPP_LEVEL_NAMES.length) return MINIAPP_LEVEL_NAMES[MINIAPP_LEVEL_NAMES.length - 1] ?? "";
  return MINIAPP_LEVEL_NAMES[level] ?? "";
}

/** Размер сегмента прогресса на максимальном уровне (L6+, endgame). */
const CUPS_SEGMENT_MAX_LEVEL = 13440;

/** Уровень 1…6 по накопленным кубкам. */
export function miniappLevelFromCups(cups: number): number {
  const c = Math.max(0, Math.floor(cups));
  let level = 1;
  for (let i = 1; i < CUP_LEVEL_STARTS.length; i++) {
    if (c >= CUP_LEVEL_STARTS[i]!) level = i + 1;
    else break;
  }
  return Math.min(level, MAX_CUP_LEVEL);
}

export type CupsLevelProgress = {
  level: number;
  /** Всего накоплено кубков. */
  totalCups: number;
  /** Кубки в текущем сегменте (от начала уровня) — для полоски. */
  cupsInSegment: number;
  /** Размер сегмента до следующего уровня (или endgame-цикл на L6). */
  cupsToNext: number;
  /** Суммарный порог кубков до следующего уровня; null на L6+ (см. nextLevelGoal). */
  nextLevelThreshold: number | null;
  /** Кубков нужно всего для следующего уровня / endgame-рубежа (для UI «425/1260»). */
  nextLevelGoal: number;
};

/** Порог кубков для следующего уровня или endgame-рубежа на L6+. */
export function nextLevelGoalCups(progress: CupsLevelProgress): number {
  if (progress.nextLevelGoal > 0) {
    return progress.nextLevelGoal;
  }
  return Math.max(progress.totalCups, 1);
}

/** Текст прогресса: текущие кубки в БД / порог следующего уровня, напр. «425/1260 кубков». */
export function formatCupsLevelProgressLabel(progress: CupsLevelProgress): string {
  return `${progress.totalCups}/${nextLevelGoalCups(progress)} кубков`;
}

/** Доля прогресса до следующего уровня (0…100) для полоски. */
export function cupsLevelProgressBarPct(progress: CupsLevelProgress): number {
  const goal = nextLevelGoalCups(progress);
  if (goal <= 0) return 0;
  return Math.min(100, (progress.totalCups / goal) * 100);
}

/**
 * Прогресс внутри уровня для полоски «кубки / до следующего уровня».
 * На L6+ знаменатель полоски — фиксированный endgame-сегмент 13 440.
 */
export function miniappCupsLevelProgress(cups: number): CupsLevelProgress {
  const c = Math.max(0, Math.floor(cups));
  const level = miniappLevelFromCups(c);
  const idx = level - 1;
  const start = CUP_LEVEL_STARTS[idx] ?? 0;
  const nextThreshold = idx + 1 < CUP_LEVEL_STARTS.length ? CUP_LEVEL_STARTS[idx + 1]! : null;
  const cupsInSegment = c - start;
  if (nextThreshold != null) {
    const segmentSize = nextThreshold - start;
    return {
      level,
      totalCups: c,
      cupsInSegment,
      cupsToNext: Math.max(1, segmentSize),
      nextLevelThreshold: nextThreshold,
      nextLevelGoal: nextThreshold,
    };
  }
  const seg = CUPS_SEGMENT_MAX_LEVEL;
  const elephant = CUP_LEVEL_STARTS[CUP_LEVEL_STARTS.length - 1] ?? 13020;
  const beyond = Math.max(0, c - elephant);
  const cycle = Math.floor(beyond / seg) + 1;
  const nextGoal = elephant + seg * cycle;
  const inSeg = cupsInSegment % seg;
  return {
    level,
    totalCups: c,
    cupsInSegment: inSeg,
    cupsToNext: seg,
    nextLevelThreshold: null,
    nextLevelGoal: nextGoal,
  };
}

/** @deprecated Используй miniappLevelFromCups; оставлено для старых импортов. */
export const MINIAPP_LEVEL_XP_STEP = 200;

/** @deprecated */
export function miniappLevelFromXp(xp: number): number {
  return miniappLevelFromCups(xp);
}
