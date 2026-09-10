/** 1 день, 2 дня, 5 дней (целое ≥ 0). */
export function daysWordRu(n: number): string {
  const abs = Math.abs(n) % 100;
  const d = Math.abs(n) % 10;
  if (abs >= 11 && abs <= 14) return "дней";
  if (d === 1) return "день";
  if (d >= 2 && d <= 4) return "дня";
  return "дней";
}

/** Для aria / подсказок к бейджу «Стрик» в ленте. */
export function streakStreakAriaLabel(days: number): string {
  if (days <= 0) return "Стрик: пока нет дней подряд с тренировками";
  return `Стрик: ${days} ${daysWordRu(days)} подряд`;
}

/** Локальная полночь для YYYY-MM-DD. */
export function parseLocalDateYMD(ymd: string): Date {
  const [y, m, d] = ymd.split("-").map(Number);
  return new Date(y, m - 1, d);
}

/** Дней с последней тренировки по дате YYYY-MM-DD и текущему локальному «сегодня». */
export function daysSinceLastTrainingFromDate(
  lastTrainingDate: string | null | undefined,
  now: Date = new Date(),
): number {
  const ltd = lastTrainingDate?.trim();
  if (!ltd) return -1;
  const last = parseLocalDateYMD(ltd);
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const diff = Math.round((today.getTime() - last.getTime()) / 86_400_000);
  return Math.max(0, diff);
}

/** Полночь после «догон»-дня: конец календарного дня, следующего за lastTrainingDate. */
export function streakBurnDeadline(lastTrainingDate: string): Date {
  const last = parseLocalDateYMD(lastTrainingDate.trim());
  const deadline = new Date(last.getFullYear(), last.getMonth(), last.getDate());
  deadline.setDate(deadline.getDate() + 2);
  return deadline;
}

/**
 * Сколько миллисекунд осталось до сгорания стрика.
 *
 * Стрик жив, пока последняя тренировка была сегодня (`daysSinceLastTraining === 0`)
 * или вчера (`=== 1`). Сгорает в полночь после «догон»-дня — см. streakBurnDeadline.
 * Если передан `lastTrainingDate` (YYYY-MM-DD), считаем по нему (точнее при тиках UI без refresh API).
 *
 * Возвращает null, если стрика нет, дата неизвестна или дедлайн уже прошёл.
 */
export function streakBurnRemainingMs(
  streak: number,
  daysSinceLastTraining: number,
  now: Date = new Date(),
  lastTrainingDate?: string | null,
): number | null {
  if (streak <= 0) return null;
  const ltd = lastTrainingDate?.trim();
  if (ltd) {
    const ms = streakBurnDeadline(ltd).getTime() - now.getTime();
    return ms > 0 ? ms : null;
  }
  if (daysSinceLastTraining < 0 || daysSinceLastTraining > 1) return null;
  const deadline = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  deadline.setDate(deadline.getDate() + (2 - daysSinceLastTraining));
  const ms = deadline.getTime() - now.getTime();
  return ms > 0 ? ms : null;
}

/** «5 ч 20 мин», «1д 4ч», «18 мин» — компактный остаток времени до сгорания. */
export function formatStreakBurnRemaining(ms: number): string {
  const totalMin = Math.max(0, Math.floor(ms / 60_000));
  const days = Math.floor(totalMin / (60 * 24));
  const hours = Math.floor((totalMin % (60 * 24)) / 60);
  const mins = totalMin % 60;
  if (days > 0) return `${days}д ${hours}ч`;
  if (hours > 0) return `${hours} ч ${mins} мин`;
  return `${mins} мин`;
}

/**
 * Согласует days_since_last_training с API и локальный пересчёт по lastTrainingDate.
 * Если дата «сырая» сильно больше значения с бэкенда — больничный заморозил стрик, доверяем API.
 */
function resolveStreakIdleDays(
  daysSinceLastTraining: number,
  now: Date,
  lastTrainingDate?: string | null,
): { days: number; frozen: boolean } {
  const ltd = lastTrainingDate?.trim();
  const daysFromDate = ltd ? daysSinceLastTrainingFromDate(ltd, now) : -1;

  if (daysSinceLastTraining >= 0) {
    const frozen = daysFromDate >= 0 && daysFromDate > daysSinceLastTraining + 1;
    if (frozen) {
      return { days: daysSinceLastTraining, frozen: true };
    }
    const days =
      daysFromDate >= 0 ? Math.max(daysFromDate, daysSinceLastTraining) : daysSinceLastTraining;
    return { days, frozen: false };
  }

  if (daysFromDate >= 0) {
    return { days: daysFromDate, frozen: false };
  }
  return { days: daysSinceLastTraining, frozen: false };
}

/**
 * Актуальный стрик для UI: 0 после полночи дня, следующего за последней тренировкой,
 * если в этот «догон» не было отчёта (см. EffectiveStreakDays на бэкенде).
 */
export function effectiveStreakDays(
  streak: number,
  daysSinceLastTraining: number,
  now: Date = new Date(),
  lastTrainingDate?: string | null,
): number {
  if (streak <= 0) return 0;
  const { days, frozen } = resolveStreakIdleDays(daysSinceLastTraining, now, lastTrainingDate);
  if (days < 0) return streak;
  if (days >= 2) return 0;
  if (frozen) return streak;
  const ms = streakBurnRemainingMs(streak, days, now, lastTrainingDate);
  if (ms == null) return 0;
  return streak;
}

/** Подпись под счётчиком стрика: «сгорит через 5 ч 20 мин» или null, если стрик не горит. */
export function streakBurnLabel(
  streak: number,
  daysSinceLastTraining: number,
  now: Date = new Date(),
  lastTrainingDate?: string | null,
): string | null {
  const { days, frozen } = resolveStreakIdleDays(daysSinceLastTraining, now, lastTrainingDate);
  if (frozen || days >= 2) return null;
  const ms = streakBurnRemainingMs(streak, days, now, lastTrainingDate);
  if (ms == null) return null;
  return `сгорит через ${formatStreakBurnRemaining(ms)}`;
}

/** Подпись к «Спасти стрик»: когда кнопка бесполезна / опасна по ошибке. */
export function streakSaveHint(
  daysSinceLastTraining: number,
  attemptsAvail: number,
): string | null {
  const reason = streakSaveWindowError(daysSinceLastTraining, attemptsAvail);
  if (!reason) return "Можно восстановить сгоревший стрик, если случайно забыл внести тренировку.";
  switch (reason) {
    case "no_attempts":
      return "Попыток нет — расти по уровням, чтобы получить ещё.";
    case "not_needed":
      return "Стрик ещё жив — сначала потренируйся, спасение не нужно.";
    case "too_late":
      return "Пропущено больше одного дня — одной попыткой не восстановить.";
    case "no_training_history":
      return "Пока не было тренировок — спасать нечего.";
    default:
      return null;
  }
}

/**
 * Окно спасения: ровно 2 календарных дня с last_training_date (стрик уже 0 в UI),
 * и есть попытки. Совпадает с UseStreakSaveAttemptForAPI на бэкенде.
 */
export function streakSaveWindowError(
  daysSinceLastTraining: number,
  attemptsAvail: number,
): "no_attempts" | "no_training_history" | "not_needed" | "too_late" | null {
  if (attemptsAvail <= 0) return "no_attempts";
  if (daysSinceLastTraining < 0) return "no_training_history";
  if (daysSinceLastTraining < 2) return "not_needed";
  if (daysSinceLastTraining > 2) return "too_late";
  return null;
}

export function canUseStreakSave(daysSinceLastTraining: number, attemptsAvail: number): boolean {
  return streakSaveWindowError(daysSinceLastTraining, attemptsAvail) === null;
}

/** Склонение «кубок» для счётчика прогресса. */
export function cupsWordRu(n: number): string {
  const abs = Math.abs(n) % 100;
  const d = Math.abs(n) % 10;
  if (abs >= 11 && abs <= 14) return "кубков";
  if (d === 1) return "кубок";
  if (d >= 2 && d <= 4) return "кубка";
  return "кубков";
}
