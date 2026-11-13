/** PRE-moderation system copy (sync with backend moderation/warnings.go). */

const MODERATION_MESSAGES: Record<string, string> = {
  moderation_profanity: "Not posted — your text contains disallowed words. Rewrite without slurs or profanity.",
  moderation_link: "Not posted — links aren’t allowed in the pack yet.",
  moderation_critical: "Not posted — this text can’t be published.",
  moderation_too_long: "Too long — shorten your text.",
  moderation_rate_limited: "Wait a moment before your next message.",
  leo_daily_limited: "Daily limit reached — 20 messages to Leo per day. You can continue tomorrow.",
  moderation_blocked: "Not posted — your text didn’t pass review.",
  user_muted: "Posting in the pack is temporarily limited. If this is a mistake, contact support.",
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
