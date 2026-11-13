/** PRE-moderation system copy (sync with backend moderation/warnings.go). */

import type { UILang } from "./i18n";

const MODERATION_MESSAGES_EN: Record<string, string> = {
  moderation_profanity: "Not posted — your text contains disallowed words. Rewrite without slurs or profanity.",
  moderation_link: "Not posted — links aren’t allowed in the pack yet.",
  moderation_critical: "Not posted — this text can’t be published.",
  moderation_too_long: "Too long — shorten your text.",
  moderation_rate_limited: "Wait a moment before your next message.",
  leo_daily_limited: "Daily limit reached — 20 messages to Leo per day. You can continue tomorrow.",
  moderation_blocked: "Not posted — your text didn’t pass review.",
  user_muted: "Posting in the pack is temporarily limited. If this is a mistake, contact support.",
};

const MODERATION_MESSAGES_RU: Record<string, string> = {
  moderation_profanity: "Не опубликовано — в тексте есть недопустимые слова. Перепиши без оскорблений и мата.",
  moderation_link: "Не опубликовано — ссылки в стае пока запрещены.",
  moderation_critical: "Не опубликовано — этот текст нельзя публиковать.",
  moderation_too_long: "Слишком длинно — сократи текст.",
  moderation_rate_limited: "Подожди немного перед следующим сообщением.",
  leo_daily_limited: "Дневной лимит исчерпан — 20 сообщений Лео в день. Продолжишь завтра.",
  moderation_blocked: "Не опубликовано — текст не прошёл проверку.",
  user_muted: "Публикации в стае временно ограничены. Если это ошибка, напиши в поддержку.",
};

const MODERATION_MESSAGES: Record<UILang, Record<string, string>> = {
  en: MODERATION_MESSAGES_EN,
  ru: MODERATION_MESSAGES_RU,
};

export function moderationUserMessage(
  code: string | undefined,
  fallback?: string,
  lang: UILang = "en",
): string {
  const map = MODERATION_MESSAGES[lang];
  if (code && map[code]) return map[code];
  if (fallback?.trim()) return fallback.trim();
  return map.moderation_blocked;
}

export function isModerationError(code: string | undefined): boolean {
  if (!code) return false;
  return code.startsWith("moderation_") || code === "user_muted" || code === "leo_daily_limited";
}
