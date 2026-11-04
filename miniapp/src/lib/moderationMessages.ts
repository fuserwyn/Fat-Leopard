/** Системные тексты PRE-модерации (синхрон с backend moderation/warnings.go). */

const MODERATION_MESSAGES: Record<string, string> = {
  moderation_profanity: "Не опубликовал — в тексте есть недопустимые слова. Перепиши без оскорблений и мата.",
  moderation_link: "Не опубликовал — ссылки в Стае пока запрещены.",
  moderation_critical: "Не опубликовал — такой текст нельзя публиковать.",
  moderation_too_long: "Слишком длинно — сократи текст.",
  moderation_rate_limited: "Подожди немного перед следующим сообщением.",
  leo_daily_limited: "Достигнут лимит — 20 сообщений Лео в день. Завтра можно продолжить.",
  moderation_blocked: "Не опубликовал — текст не прошёл проверку.",
  user_muted: "Публикация в Стае временно ограничена. Если это ошибка — напиши в поддержку.",
};

export function moderationUserMessage(code: string | undefined, fallback?: string): string {
  if (code && MODERATION_MESSAGES[code]) return MODERATION_MESSAGES[code];
  if (fallback?.trim()) return fallback.trim();
  return MODERATION_MESSAGES.moderation_blocked;
}

export function isModerationError(code: string | undefined): boolean {
  if (!code) return false;
  return code.startsWith("moderation_") || code === "user_muted" || code === "leo_daily_limited";
}
